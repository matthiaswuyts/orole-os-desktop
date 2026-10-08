import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Session } from "@/mygo";

const api = vi.hoisted(() => ({ sendMessage: vi.fn(), options: vi.fn(), cancelQueuedPrompt: vi.fn(), resumeQueue: vi.fn(), upsert: vi.fn() }));
vi.mock("@/mygo", () => ({ Agents: api }));
vi.mock("@/stores/agents", () => ({ useAgents: { getState: () => ({ upsert: api.upsert }) } }));
vi.mock("@/features/agents/shared", () => ({ ErrorNote: ({ error }: { error: string | null }) => error ? <p role="alert">{error}</p> : null }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/stores/daemon", () => ({ useDaemon: (select: (state: { link: string }) => unknown) => select({ link: "connected" }) }));
import { Composer } from "./Composer";
import { composerState } from "./composerState";
import { MessageQueue } from "./MessageQueue";

const session = (patch: Partial<Session> = {}): Session => ({ id: "task-a", mode: "structured", machine: "local", live: true, status: "working", pending: [], questions: [], queue: [], ...patch }) as Session;
beforeEach(() => { vi.clearAllMocks(); api.sendMessage.mockResolvedValue(session()); api.options.mockResolvedValue({ harnesses: [] }); });
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
