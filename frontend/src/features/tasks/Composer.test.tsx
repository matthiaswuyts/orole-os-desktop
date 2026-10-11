import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { HarnessOption, PermissionModeSupport, Session } from "@/mygo";

const api = vi.hoisted(() => ({ sendMessage: vi.fn(), options: vi.fn(), cancelQueuedPrompt: vi.fn(), resumeQueue: vi.fn(), upsert: vi.fn(), setPermissionMode: vi.fn() }));
vi.mock("@/mygo", () => ({ Agents: api }));
vi.mock("@/stores/agents", () => ({ useAgents: { getState: () => ({ upsert: api.upsert }) } }));
vi.mock("@/features/agents/shared", () => ({ ErrorNote: ({ error }: { error: string | null }) => error ? <p role="alert">{error}</p> : null }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/stores/daemon", () => ({ useDaemon: (select: (state: { link: string }) => unknown) => select({ link: "connected" }) }));
import { toastManager } from "@/components/ui/toast";
import { useHarnessOptionsCache } from "@/stores/harnessOptions";
import { Composer } from "./Composer";
import { composerState } from "./composerState";
import { MessageQueue } from "./MessageQueue";

const session = (patch: Partial<Session> = {}): Session => ({ id: "task-a", mode: "structured", machine: "local", live: true, status: "working", pending: [], questions: [], queue: [], ...patch }) as Session;
beforeEach(() => { vi.clearAllMocks(); useHarnessOptionsCache.setState({ machines: {} }); api.sendMessage.mockResolvedValue(session()); api.options.mockResolvedValue({ harnesses: [] }); });
afterEach(cleanup);

describe("task composer delivery", () => {
  it("preserves a busy task's draft on rejection and clears it only after acceptance", async () => {
    api.sendMessage.mockRejectedValueOnce(new Error("Daemon disconnected"));
    render(<Composer session={session()} />);
    fireEvent.change(screen.getByLabelText("Message the agent"), { target: { value: "  Keep this draft  " } });
    fireEvent.click(screen.getByRole("button", { name: "Queue message" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Daemon disconnected");
    expect(screen.getByLabelText("Message the agent")).toHaveValue("  Keep this draft  ");
    fireEvent.click(screen.getByRole("button", { name: "Queue message" }));
    await waitFor(() => expect(screen.getByLabelText("Message the agent")).toHaveValue(""));
    expect(api.sendMessage).toHaveBeenLastCalledWith("task-a", "Keep this draft", [], "", "");
  });

  it("does not submit composition Enter or Shift+Enter and deduplicates pending sends", async () => {
    const queued = Promise.withResolvers<Session>();
    api.sendMessage.mockReturnValue(queued.promise);
    render(<Composer session={session()} />);
    const input = screen.getByLabelText("Message the agent");
    fireEvent.change(input, { target: { value: "next" } });
    fireEvent.keyDown(input, { key: "Enter", isComposing: true });
    fireEvent.keyDown(input, { key: "Enter", shiftKey: true });
    expect(api.sendMessage).not.toHaveBeenCalled();
    fireEvent.keyDown(input, { key: "Enter" });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(api.sendMessage).toHaveBeenCalledTimes(1);
    queued.resolve(session());
    await waitFor(() => expect(input).toHaveValue(""));
  });

  it("inserts Ctrl+Enter at the selection without sending, then sends the multiline draft with Enter", async () => {
    render(<Composer session={session()} />);
    const input = screen.getByLabelText("Message the agent") as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: "first replace last" } });
    input.setSelectionRange(6, 13);
    fireEvent.keyDown(input, { key: "Enter", ctrlKey: true });
    expect(input).toHaveValue("first \n last");
    expect(api.sendMessage).not.toHaveBeenCalled();
    fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(() => expect(input).toHaveValue(""));
    expect(api.sendMessage).toHaveBeenCalledWith("task-a", "first \n last", [], "", "");
  });

  it("allows a stopped task's follow-up without automatically releasing its paused queue", async () => {
    render(<Composer session={session({ live: false, status: "stopped", queue: [{ id: "q", text: "later", state: "paused", error: "Stopped", createdAt: "2026-10-09T00:00:00Z" }] })} />);
    fireEvent.change(screen.getByLabelText("Message the agent"), { target: { value: "resume now" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await waitFor(() => expect(api.sendMessage).toHaveBeenCalledWith("task-a", "resume now", [], "", ""));
    expect(screen.getByRole("button", { name: "Resume queue" })).toBeDisabled();
  });

  it("keeps terminal and remote transport restrictions and separates questions from messages", () => {
    expect(composerState(session({ mode: "terminal" })).canSend).toBe(false);
    expect(composerState(session({ mode: "process", machine: "remote" })).canSend).toBe(false);
    expect(composerState(session({ mode: "process", machine: "remote", live: false, status: "lost" })).canSend).toBe(false);
    expect(composerState(session({ mode: "process", machine: "remote", live: false, status: "done" }))).toMatchObject({ delivery: "prompt", label: "Start task" });
    expect(composerState(session({ status: "needs-you" }))).toMatchObject({ delivery: "queue", label: "Queue message" });
    expect(composerState(session({ status: "idle" }))).toMatchObject({ delivery: "prompt" });
  });

  it("cannot clear another task's draft when an old send resolves", async () => {
    const queued = Promise.withResolvers<Session>();
    api.sendMessage.mockReturnValue(queued.promise);
    const view = render(<Composer session={session()} />);
    fireEvent.change(screen.getByLabelText("Message the agent"), { target: { value: "old draft" } });
    fireEvent.click(screen.getByRole("button", { name: "Queue message" }));
    view.rerender(<Composer session={session({ id: "task-b" })} />);
    fireEvent.change(screen.getByLabelText("Message the agent"), { target: { value: "new draft" } });
    queued.resolve(session());
    await waitFor(() => expect(api.upsert).toHaveBeenCalled());
    expect(screen.getByLabelText("Message the agent")).toHaveValue("new draft");
  });
});

describe("daemon queue receipts", () => {
  const entry = { id: "queued-1", text: "follow-up", state: "paused", error: "Turn failed", createdAt: "2026-10-09T00:00:00Z" };
  it("resumes only eligible paused queues and keeps failures visible", async () => {
    api.resumeQueue.mockRejectedValue(new Error("Still waiting for a tool"));
    render(<MessageQueue session={session({ status: "failed", queue: [entry] })} />);
    fireEvent.click(screen.getByRole("button", { name: "Resume queue" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Still waiting for a tool");
    expect(api.resumeQueue).toHaveBeenCalledWith("task-a");
    expect(screen.getByText("follow-up")).toBeInTheDocument();
    expect(screen.getByText("Turn failed")).toBeInTheDocument();
  });

  it("never retries uncertain delivery and cannot cancel a sending entry", async () => {
    api.cancelQueuedPrompt.mockResolvedValue(session());
    render(<MessageQueue session={session({ status: "done", queue: [entry, { ...entry, id: "uncertain", state: "uncertain", text: "maybe sent" }, { ...entry, id: "sending", state: "sending", text: "sending now" }] })} />);
    expect(screen.getByRole("button", { name: "Resume queue" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel queued message: sending now" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Remove uncertain receipt: maybe sent" }));
    await waitFor(() => expect(api.cancelQueuedPrompt).toHaveBeenCalledWith("task-a", "uncertain"));
    expect(api.resumeQueue).not.toHaveBeenCalled();
  });
});

describe("permission mode", () => {
  const modes: PermissionModeSupport[] = [
    { mode: "manual", available: true, approximated: false, detail: "" },
    { mode: "auto", available: true, approximated: false, detail: "Claude Code's own classifier decides." },
    { mode: "yolo", available: true, approximated: false, detail: "" },
  ];
  const harness = (permissionModes: PermissionModeSupport[]) => ({ id: "claude", name: "Claude Code", available: true, modes: ["structured"], structured: true, models: [], reasoning: [], permissionModes }) as unknown as HarnessOption;
  const switchable = { attachments: true, model: true, reasoning: true, reason: "", permissionMode: true, permissionModeRestarts: false, permissionModeReason: "" };
  const live = (patch: Partial<Session> = {}) => session({ kind: "claude", permissionMode: "manual", followup: switchable, ...patch } as Partial<Session>);
  const trigger = (name: RegExp | string = /^Permission mode/) => screen.getByRole("combobox", { name });
  // The harness's mode report arrives with the launch options; wait for it so options reflect it.
  // Base UI ignores a release that follows the opening press too closely, so settle before choosing.
  const pick = async (name: RegExp) => {
    const option = await screen.findByRole("option", { name });
    await new Promise((resolve) => setTimeout(resolve, 400));
    fireEvent.pointerUp(option, { button: 0, pointerType: "mouse" });
    fireEvent.mouseUp(option, { button: 0 });
    fireEvent.click(option);
  };
  const open = async (name: RegExp | string = /^Permission mode/) => {
    await waitFor(() => expect(useHarnessOptionsCache.getState().machines.local?.options).toBeDefined());
    fireEvent.click(trigger(name));
  };

  beforeEach(() => { api.options.mockResolvedValue({ harnesses: [harness(modes)] }); api.setPermissionMode.mockImplementation(async (_id: string, mode: string) => live({ permissionMode: mode })); });

  it("shows the task's mode and describes Auto as the agent deciding", async () => {
    render(<Composer session={live({ permissionMode: "auto" })} />);
    expect(trigger("Permission mode: Auto")).toBeVisible();
    await open("Permission mode: Auto");
    expect(await screen.findByRole("option", { name: /Auto.*The agent decides which actions are safe/ })).toBeVisible();
    expect(screen.getByRole("option", { name: /Manual.*Asks before anything it isn't sure is safe/ })).toBeVisible();
    expect(screen.getByRole("option", { name: /YOLO.*Never asks/ })).toBeVisible();
  });

  it("treats a stored legacy value as Manual", () => {
    render(<Composer session={live({ permissionMode: "default" })} />);
    expect(trigger("Permission mode: Manual")).toBeVisible();
  });

  it("switches a running task through the daemon and publishes the returned session", async () => {
    render(<Composer session={live()} />);
    await open();
    await pick(/Auto/);
    await waitFor(() => expect(api.setPermissionMode).toHaveBeenCalledWith("task-a", "auto", false));
    await waitFor(() => expect(api.upsert).toHaveBeenCalledWith(expect.objectContaining({ permissionMode: "auto" })));
  });

  it("asks for confirmation before YOLO and sends confirmYolo only after it", async () => {
    render(<Composer session={live()} />);
    await open();
    await pick(/YOLO/);
    expect(await screen.findByText("Run without asking?")).toBeVisible();
    expect(screen.getByText(/No prompts will be shown/)).toBeVisible();
    expect(api.setPermissionMode).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByText("Run without asking?")).toBeNull());
    expect(api.setPermissionMode).not.toHaveBeenCalled();

    await open();
    await pick(/YOLO/);
    fireEvent.click(await screen.findByRole("button", { name: "Enable YOLO" }));
    await waitFor(() => expect(api.setPermissionMode).toHaveBeenCalledWith("task-a", "yolo", true));
    await waitFor(() => expect(screen.queryByText("Run without asking?")).toBeNull());
  });

  it("keeps the dialog open and shows the daemon's refusal when YOLO is rejected", async () => {
    api.setPermissionMode.mockRejectedValueOnce(new Error("bad_request: YOLO mode is turned off in Orole's settings"));
    render(<Composer session={live()} />);
    await open();
    await pick(/YOLO/);
    fireEvent.click(await screen.findByRole("button", { name: "Enable YOLO" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("YOLO mode is turned off in Orole's settings");
    expect(screen.getByText("Run without asking?")).toBeVisible();
  });

  it("reports a rejected switch to Auto as a toast and keeps the old mode", async () => {
    const toast = vi.spyOn(toastManager, "add").mockReturnValue("t");
    api.setPermissionMode.mockRejectedValueOnce(new Error("internal: wait for the current turn to finish"));
    render(<Composer session={live()} />);
    await open();
    await pick(/Auto/);
    await waitFor(() => expect(toast).toHaveBeenCalledWith(expect.objectContaining({ type: "error", description: expect.stringContaining("wait for the current turn to finish") })));
    expect(api.upsert).not.toHaveBeenCalled();
    expect(trigger("Permission mode: Manual")).toBeVisible();
    toast.mockRestore();
  });

  it("marks an approximated Auto and a disabled YOLO honestly", async () => {
    api.options.mockResolvedValue({ harnesses: [harness([
      modes[0]!,
      { mode: "auto", available: true, approximated: true, detail: "No AI reviewer here; Auto approves file edits only." },
      { mode: "yolo", available: false, approximated: false, detail: "YOLO mode is turned off in Orole's settings." },
    ])] });
    render(<Composer session={live()} />);
    await open();
    expect(await screen.findByRole("option", { name: /Auto.*Approximated.*No AI reviewer here/ })).toBeVisible();
    const yolo = screen.getByRole("option", { name: /YOLO.*turned off in Orole's settings/ });
    expect(yolo).toHaveAttribute("aria-disabled", "true");
    fireEvent.click(yolo);
    expect(screen.queryByText("Run without asking?")).toBeNull();
  });

  it("is disabled with the daemon's reason when the task cannot change mode", async () => {
    render(<Composer session={live({ mode: "process", machine: "box", live: false, status: "done", followup: { ...switchable, permissionMode: false, permissionModeReason: "Remote one-shot runs cannot change permission mode; start a new task." } })} />);
    const select = trigger();
    expect(select).toBeDisabled();
    const wrapper = select.parentElement!;
    fireEvent.pointerEnter(wrapper, { pointerType: "mouse" });
    fireEvent.pointerMove(wrapper, { pointerType: "mouse" });
    fireEvent.mouseEnter(wrapper);
    fireEvent.mouseMove(wrapper);
    expect(await screen.findByText("Remote one-shot runs cannot change permission mode; start a new task.", {}, { timeout: 3000 })).toBeVisible();
    fireEvent.click(select);
    expect(screen.queryByRole("option")).toBeNull();
  });

  it("shows a read-only review session as locked", () => {
    render(<Composer session={live({ permissionMode: "plan", followup: { ...switchable, permissionMode: false, permissionModeReason: "Read-only review sessions keep their mode." } })} />);
    expect(trigger("Permission mode: Read-only")).toBeDisabled();
  });
});
