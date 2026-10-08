import { act, cleanup, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { Attachment, HarnessOption, LaunchOptions, Session } from "@/mygo";

const api = vi.hoisted(() => ({ options: vi.fn(), prompt: vi.fn(), sendMessage: vi.fn(), uploadAttachment: vi.fn(), discardAttachment: vi.fn(), upsert: vi.fn(), toast: vi.fn() }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({ Agents: api }));
vi.mock("@/stores/agents", () => ({ useAgents: { getState: () => ({ upsert: api.upsert }) } }));
vi.mock("@/stores/daemon", () => ({ useDaemon: (select: (state: { link: string }) => unknown) => select({ link: "connected" }) }));
vi.mock("@/features/projects/dialogs", () => ({ toastError: api.toast }));
import { useTaskMessage } from "./useTaskMessage";
import { TaskAttachmentButton, TaskAttachmentPreviews, TaskModelPicker } from "./TaskMessageControls";

const harness: HarnessOption = {
  id: "codex", name: "Codex", available: true, executable: "codex", version: "1", reasoning: ["low", "high"], customModel: true,
  modes: ["structured"], structured: true, interactive: false, unavailableReason: "",
  models: [
    { id: "model-a", name: "Model A", reasoning: ["low", "high"], default: true },
    { id: "model-b", name: "Model B", reasoning: ["low", "medium"], default: false },
    { id: "model-c", name: "Model C", reasoning: [], default: false },
  ],
};
const options: LaunchOptions = { machine: "local", harnesses: [harness, { ...harness, id: "claude", name: "Claude Code" }], projects: [], directories: [], worktree: true, unavailableReason: "" };
const session = (patch: Partial<Session> = {}): Session => ({
  id: "task-a", kind: "codex", machine: "local", model: "model-a", reasoning: "high", mode: "structured", live: true, status: "working",
  pending: [], questions: [], queue: [], followup: { attachments: true, model: true, reasoning: true, reason: "" }, ...patch,
}) as Session;

beforeEach(() => {
  vi.clearAllMocks();
  api.options.mockResolvedValue(options);
  api.sendMessage.mockResolvedValue(session());
  api.uploadAttachment.mockResolvedValue({ id: "upload-one", name: "notes.txt", size: 8, mime: "text/plain" });
  api.discardAttachment.mockResolvedValue(undefined);
});
afterEach(cleanup);

it("discovers the session machine and sends selected controls through the daemon's authoritative message API", async () => {
  const receipt = session({ queue: [{ id: "queued", text: "next", state: "queued", error: "", createdAt: "2026-10-09T00:00:00Z" }] });
  api.sendMessage.mockResolvedValue(receipt);
  const { result } = renderHook(() => useTaskMessage(session()));
  await waitFor(() => expect(result.current.canChooseModel).toBe(true));
  expect(api.options).toHaveBeenCalledWith("local");
  expect(result.current.harness?.id).toBe("codex");
  act(() => result.current.chooseModel("model-b"));
  expect(result.current.reasoning).toBe("low");
  act(() => result.current.chooseReasoning("medium"));
  await act(async () => result.current.send("  next  "));
  expect(api.sendMessage).toHaveBeenCalledWith("task-a", "next", [], "model-b", "medium");
  expect(api.upsert).toHaveBeenCalledWith(receipt);
});

it("never clears explicit effort through an empty override or selects an incompatible model", async () => {
  const { result } = renderHook(() => useTaskMessage(session()));
  await waitFor(() => expect(result.current.canChooseModel).toBe(true));
  act(() => result.current.chooseReasoning(""));
  expect(result.current.reasoning).toBe("high");
  expect(result.current.modelBlockedReason(harness.models[2]!)).toMatch(/cannot preserve/);
  act(() => result.current.chooseModel("model-c"));
  expect(result.current.model).toBe("model-a");
  await act(async () => result.current.send("keep settings"));
  expect(api.sendMessage).toHaveBeenCalledWith("task-a", "keep settings", [], "model-a", "high");
});

it("captures the displayed settings even when an earlier queued message selected a different model", async () => {
  const { result } = renderHook(() => useTaskMessage(session()));
  await waitFor(() => expect(result.current.canChooseModel).toBe(true));
  act(() => result.current.chooseModel("model-b"));
  await act(async () => result.current.send("first"));
  await act(async () => result.current.send("second"));
  expect(api.sendMessage).toHaveBeenNthCalledWith(1, "task-a", "first", [], "model-b", "low");
  expect(api.sendMessage).toHaveBeenNthCalledWith(2, "task-a", "second", [], "model-a", "high");
});

it("preserves the explicitly labelled fresh-task flow for an ended remote process", async () => {
  const remote = session({ mode: "process", machine: "remote", live: false, status: "done", followup: { attachments: false, model: false, reasoning: false, reason: "Remote process" } });
  api.prompt.mockResolvedValue(remote);
  const { result } = renderHook(() => useTaskMessage(remote));
  await act(async () => result.current.send("new task"));
  expect(api.prompt).toHaveBeenCalledWith("task-a", "new task");
  expect(api.sendMessage).not.toHaveBeenCalled();
  expect(result.current.attachments.canAdd).toBe(false);
});

it("blocks incomplete uploads, retains files on failure, and clears attachment-only drafts only after acceptance", async () => {
  const upload = Promise.withResolvers<Attachment>();
  api.uploadAttachment.mockReturnValue(upload.promise);
  const { result } = renderHook(() => useTaskMessage(session()));
  act(() => { void result.current.attachments.add([new File(["research"], "notes.txt", { type: "text/plain" })]); });
  await waitFor(() => expect(api.uploadAttachment).toHaveBeenCalled());
  await act(async () => { await expect(result.current.send("")).rejects.toThrow("Wait for attachments"); });
  expect(api.sendMessage).not.toHaveBeenCalled();
  await act(async () => { upload.resolve({ id: "upload-one", name: "notes.txt", size: 8, mime: "text/plain" }); });
  await waitFor(() => expect(result.current.attachments.ready).toBe(true));
  api.sendMessage.mockRejectedValueOnce(new Error("Daemon disconnected"));
  await act(async () => { await expect(result.current.send("")).rejects.toThrow("Daemon disconnected"); });
  expect(result.current.error).toBe("Daemon disconnected");
  expect(result.current.attachments.ids).toEqual(["upload-one"]);
  expect(api.discardAttachment).not.toHaveBeenCalled();
  await act(async () => result.current.send(""));
  expect(api.sendMessage).toHaveBeenLastCalledWith("task-a", "", ["upload-one"], "model-a", "high");
  expect(result.current.attachments.files).toEqual([]);
});

it("guards duplicate dispatches and disables attachment changes while sending", async () => {
  const pending = Promise.withResolvers<Session>();
  api.sendMessage.mockReturnValue(pending.promise);
  const { result } = renderHook(() => useTaskMessage(session()));
  let first!: Promise<void>;
  act(() => { first = result.current.send("next"); });
  expect(result.current.busy).toBe(true);
  expect(result.current.attachments.canAdd).toBe(false);
  await act(async () => { await expect(result.current.send("next")).rejects.toThrow("already being sent"); });
  expect(api.sendMessage).toHaveBeenCalledTimes(1);
  await act(async () => { pending.resolve(session()); await first; });
  expect(result.current.busy).toBe(false);
});

function Controls({ value }: { value: Session }) {
  const message = useTaskMessage(value);
  return <><TaskAttachmentButton message={message} /><TaskAttachmentPreviews message={message} /><TaskModelPicker message={message} /></>;
}

it("locks the provider and exposes disabled model controls with the daemon's explanation", async () => {
  render(<Controls value={session({ followup: { attachments: false, model: false, reasoning: false, reason: "This live conversation cannot change model or reasoning." } })} />);
  expect(screen.getByRole("button", { name: "Add attachment" })).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "Model and reasoning" }));
  expect(await screen.findByText("This live conversation cannot change model or reasoning.")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Change model" })).toBeDisabled();
  expect(screen.queryByRole("button", { name: "Change provider" })).not.toBeInTheDocument();
  expect(screen.queryByText("Claude Code")).not.toBeInTheDocument();
});

it("uses the existing searchable picker and only offers actual effort overrides", async () => {
  render(<Controls value={session()} />);
  fireEvent.click(screen.getByRole("button", { name: "Model and reasoning" }));
  const changeModel = await screen.findByRole("button", { name: "Change model" });
  await waitFor(() => expect(changeModel).toBeEnabled());
  const slider = screen.getByRole("slider", { name: "Reasoning effort" });
  expect(slider).toHaveAttribute("aria-valuetext", "High");
  fireEvent.change(slider, { target: { value: "0" } });
  expect(slider).toHaveAttribute("aria-valuetext", "Low");
  fireEvent.click(changeModel);
  expect(await screen.findByLabelText("Search models")).toBeInTheDocument();
  fireEvent.click(await screen.findByRole("option", { name: "Model B" }));
  expect(screen.getByRole("button", { name: "Model and reasoning" })).toHaveTextContent("Model B");
});
