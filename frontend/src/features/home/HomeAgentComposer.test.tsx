import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { HarnessOption, LaunchOptions, Machine, Project, Worktree } from "@/mygo";
const api = vi.hoisted(() => ({ options: vi.fn(), start: vi.fn(), upload: vi.fn(), discard: vi.fn(), toast: vi.fn() }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({ Agents: { options: api.options, start: api.start, uploadAttachment: api.upload, discardAttachment: api.discard }, events: {} }));
import { useDaemon } from "@/stores/daemon";
import { useHarnessOptionsCache } from "@/stores/harnessOptions";
vi.mock("@/features/projects/dialogs", async (original) => ({ ...(await original<object>()), toastError: api.toast }));
import { useAgents } from "@/stores/agents";
import { useUI } from "@/stores/ui";
import { retryLaunch } from "@/features/tasks/pendingLaunch";
import { useProjects } from "@/stores/projects";
import { useBoxes } from "@/stores/boxes";
import { HomeAgentComposer } from "./HomeAgentComposer";
const project: Project = { id: "notes", name: "Research", machineId: "local", path: "", remoteUrl: "", lastOpened: 0, createdAt: 0, worktreeRoot: "", worktreeRootCustom: false, missing: false, isGit: false };
const harness: HarnessOption = { id: "codex", name: "Codex", available: true, executable: "codex", version: "1", reasoning: ["low", "high"], customModel: true, modes: ["structured"], structured: true, interactive: false, unavailableReason: "", permissionModes: [], models: [{ id: "model-a", name: "Model A", reasoning: ["low", "high"], default: true }, { id: "model-b", name: "Model B", reasoning: [], default: false }] };
const options: LaunchOptions = { machine: "local", harnesses: [harness], projects: [], directories: [], worktree: true, unavailableReason: "" };
beforeEach(() => {
 useDaemon.setState({ link: "connected", connectEpoch: 0 }); useHarnessOptionsCache.setState({ machines: {} });
 vi.clearAllMocks(); localStorage.clear(); api.options.mockResolvedValue(options); api.start.mockReset(); api.start.mockResolvedValue({ id: "task", updatedAt: "2026-10-08T00:00:00Z" });
 useAgents.setState({ sessions: {}, launches: {}, launchDraft: null, openId: null }); useUI.setState({ view: "home" });
 api.upload.mockResolvedValue({ id: "upload-one", name: "notes.txt", size: 8, mime: "text/plain" }); api.discard.mockResolvedValue(undefined);
 useBoxes.setState({ machines: [{ id: "local", name: "devl" }, { id: "remote", name: "gpu" }] as Machine[], observations: {} });
 useProjects.setState({ projects: [project], worktrees: {}, selectedId: null, available: true, loaded: true, newProjectOpen: false });
});
afterEach(cleanup);
async function fill() { fireEvent.change(screen.getByLabelText("Task for your agent"), { target: { value: "Plan this research" } }); const start = screen.getByRole("button", { name: "Start task" }); await waitFor(() => expect(start).toBeEnabled()); return start; }
function menu(name: string) { fireEvent.click(screen.getByRole("button", { name })); }
it("shows the task page at once and opens the started task when the daemon answers", async () => {
 const started = Promise.withResolvers<{ id: string; updatedAt: string }>(); api.start.mockReturnValue(started.promise);
 render(<HomeAgentComposer />); fireEvent.click(await fill());
 expect(useUI.getState().view).toBe("agents");
 const [launch] = Object.values(useAgents.getState().launches);
 expect(launch).toMatchObject({ status: "starting", title: "Plan this research", prompt: "Plan this research", kind: "codex", worktree: false });
 expect(useAgents.getState().openId).toBe(launch!.id);
 started.resolve({ id: "task", updatedAt: "2026-10-08T00:00:00Z" });
 await waitFor(() => expect(useAgents.getState().openId).toBe("task"));
 expect(useAgents.getState().launches).toEqual({}); expect(useAgents.getState().sessions.task).toBeDefined();
 expect(api.start).toHaveBeenCalledWith(expect.objectContaining({ projectId: "notes", dir: "", machine: "local", mode: "structured", model: "model-a", prompt: "Plan this research", newWorktree: false }));
});
it("keeps missing attached folders unavailable", async () => {
 useProjects.setState({ projects: [{ ...project, path: "C:\\missing", missing: true }] }); render(<HomeAgentComposer />);
 fireEvent.change(screen.getByLabelText("Task for your agent"), { target: { value: "Do something" } });
 await screen.findByRole("button", { name: "Agent, model and reasoning" });
 expect(screen.getByRole("button", { name: "Start task" })).toBeDisabled();
});
it("opens project creation without discarding the prompt", async () => {
 render(<HomeAgentComposer />); await fill(); menu("Project"); fireEvent.click(await screen.findByRole("menuitem", { name: /Create new project/ }));
 expect(useProjects.getState().newProjectOpen).toBe(true); expect(screen.getByLabelText("Task for your agent")).toHaveValue("Plan this research");
});
it("launches in the selected existing worktree rather than creating another", async () => {
 useProjects.setState({ projects: [{ ...project, path: "C:\\repo", isGit: true }], worktrees: { notes: [{ id: "wt", projectId: "notes", branch: "fix-login", path: "C:\\trees\\fix", isMain: false, error: "" } as Worktree] } });
 render(<HomeAgentComposer />); const start = await fill(); menu("Worktree"); fireEvent.click(await screen.findByRole("menuitemradio", { name: "fix-login" })); fireEvent.click(start);
 await waitFor(() => expect(api.start).toHaveBeenCalledWith(expect.objectContaining({ projectId: "notes", dir: "C:\\trees\\fix", newWorktree: false })));
});
it("drops unsupported reasoning when changing models", async () => {
 render(<HomeAgentComposer />); const start = await fill(); menu("Agent, model and reasoning");
 const slider = await screen.findByRole("slider", { name: "Reasoning effort" });
 fireEvent.change(slider, { target: { value: "2" } });
 expect(slider).toHaveAttribute("aria-valuetext", "High");
 fireEvent.click(screen.getByRole("button", { name: "Change model" }));
 fireEvent.click(await screen.findByRole("option", { name: "Model B" }));
 fireEvent.click(screen.getByRole("button", { name: "Agent, model and reasoning" })); fireEvent.click(start);
 await waitFor(() => expect(api.start).toHaveBeenCalledWith(expect.objectContaining({ model: "model-b", reasoning: "" })));
});
it("switches system and launches only with its approved project and process capabilities", async () => {
 api.options.mockImplementation(async (machine: string) => machine === "local" ? options : { ...options, machine, worktree: false, harnesses: [{ ...harness, structured: false, modes: ["process"] }], projects: [{ id: "remote:/work", name: "Remote work", path: "/work", machine, git: true }] });
 render(<HomeAgentComposer />); await fill(); menu("Execution system"); fireEvent.click(await screen.findByRole("menuitemradio", { name: /gpu/ }));
 await screen.findByText("Remote work"); const start = screen.getByRole("button", { name: "Start task" }); await waitFor(() => expect(start).toBeEnabled()); fireEvent.click(start);
 await waitFor(() => expect(api.start).toHaveBeenCalledWith(expect.objectContaining({ machine: "remote", projectId: "", dir: "/work", mode: "process", newWorktree: false })));
});
it("keeps the same request key after an uncertain failure", async () => {
 api.start.mockRejectedValueOnce(new Error("connection lost")).mockResolvedValueOnce({ id: "retried", updatedAt: "2026-10-08T00:00:00Z" });
 render(<HomeAgentComposer />); fireEvent.click(await fill());
 await waitFor(() => expect(Object.values(useAgents.getState().launches)[0]?.status).toBe("failed"));
 retryLaunch(Object.keys(useAgents.getState().launches)[0]!);
 await waitFor(() => expect(useAgents.getState().openId).toBe("retried")); expect(api.start.mock.calls[1]![0].key).toBe(api.start.mock.calls[0]![0].key);
});

it("does not send incomplete attachments and hands them to the failed launch, which keeps them staged", async () => {
 const upload = Promise.withResolvers<{ id: string; name: string; size: number; mime: string }>();
 api.upload.mockReturnValue(upload.promise); api.start.mockRejectedValueOnce(new Error("Disconnected"));
 render(<HomeAgentComposer />); const start = await fill();
 fireEvent.change(screen.getByLabelText("Attach files"), { target: { files: [new File(["research"], "notes.txt", { type: "text/plain" })] } });
 await waitFor(() => expect(api.upload).toHaveBeenCalled());
 expect(start).toBeDisabled();
 upload.resolve({ id: "uploaded-notes", name: "notes.txt", size: 8, mime: "text/plain" });
 await waitFor(() => expect(start).toBeEnabled()); fireEvent.click(start);
 await waitFor(() => expect(Object.values(useAgents.getState().launches)[0]?.status).toBe("failed"));
 const [launch] = Object.values(useAgents.getState().launches);
 expect(launch!.attachments.map((file) => file.uploadId)).toEqual(["uploaded-notes"]);
 expect(launch!.draft.prompt).toBe("Plan this research");
 expect(api.discard).not.toHaveBeenCalled();
 expect(api.start).toHaveBeenCalledWith(expect.objectContaining({ attachmentIds: ["uploaded-notes"] }));
});

it("discards an upload completed after its chip was removed", async () => {
 const upload = Promise.withResolvers<{ id: string; name: string; size: number; mime: string }>();
 api.upload.mockReturnValue(upload.promise);
 render(<HomeAgentComposer />); await fill();
 fireEvent.change(screen.getByLabelText("Attach files"), { target: { files: [new File(["research"], "notes.txt")] } });
 await waitFor(() => expect(api.upload).toHaveBeenCalled());
 fireEvent.click(screen.getByRole("button", { name: "Remove notes.txt" }));
 upload.resolve({ id: "removed-input", name: "notes.txt", size: 8, mime: "text/plain" });
 await waitFor(() => expect(api.discard).toHaveBeenCalledWith("removed-input"));
 expect(screen.queryByText("notes.txt")).not.toBeInTheDocument();
 fireEvent.click(screen.getByRole("button", { name: "Start task" }));
 await waitFor(() => expect(api.start).toHaveBeenCalledWith(expect.objectContaining({ attachmentIds: [] })));
});

it("rejects oversized files without sending their bytes and lets the user recover", async () => {
 render(<HomeAgentComposer />); const start = await fill();
 const file = new File([new Uint8Array(2 * 1024 * 1024 + 1)], "large.png", { type: "image/png" });
 fireEvent.change(screen.getByLabelText("Attach files"), { target: { files: [file] } });
 await waitFor(() => expect(api.toast).toHaveBeenCalled());
 expect(api.upload).not.toHaveBeenCalled(); expect(start).toBeDisabled();
 fireEvent.click(screen.getByRole("button", { name: "Remove large.png" }));
 expect(start).toBeEnabled();
});

async function pickProvider(name: string) {
 fireEvent.click(screen.getByRole("button", { name: "Change provider" }));
 fireEvent.click(await screen.findByRole("option", { name }));
}
async function pickModel(name: string) {
 fireEvent.click(screen.getByRole("button", { name: "Change model" }));
 fireEvent.click(await screen.findByRole("option", { name }));
}
it("restores each provider's model and effort after switching and reopening Home", async () => {
 const codex = { ...harness, models: [...harness.models!, { id: "model-c", name: "Model C", reasoning: ["low", "high"], default: false }] };
 const claude = { ...harness, id: "claude", name: "Claude Code", models: [{ id: "opus", name: "Opus 5.5", reasoning: ["low", "high"], default: true }] };
 api.options.mockResolvedValue({ ...options, harnesses: [codex, claude] });
 const view = render(<HomeAgentComposer />); await fill(); menu("Agent, model and reasoning");
 await pickModel("Model C");
 fireEvent.change(screen.getByRole("slider", { name: "Reasoning effort" }), { target: { value: "2" } });
 await pickProvider("Claude Code");
 fireEvent.change(screen.getByRole("slider", { name: "Reasoning effort" }), { target: { value: "1" } });
 await pickProvider("Codex");
 expect(screen.getByRole("slider", { name: "Reasoning effort" })).toHaveAttribute("aria-valuetext", "High");
 expect(screen.getByRole("button", { name: "Change model" })).toHaveTextContent("Model C");
 await pickProvider("Claude Code");
 expect(screen.getByRole("slider", { name: "Reasoning effort" })).toHaveAttribute("aria-valuetext", "Low");
 await pickProvider("Codex"); view.unmount();
 render(<HomeAgentComposer />); const start = await fill();
 expect(screen.getByRole("button", { name: "Agent, model and reasoning" })).toHaveTextContent("Model C");
 fireEvent.click(start);
 await waitFor(() => expect(api.start).toHaveBeenCalledWith(expect.objectContaining({ kind: "codex", model: "model-c", reasoning: "high" })));
});

it("retains preferred effort through unsupported models but remembers an explicit Auto choice", async () => {
 const view = render(<HomeAgentComposer />); await fill(); menu("Agent, model and reasoning");
 fireEvent.change(await screen.findByRole("slider", { name: "Reasoning effort" }), { target: { value: "2" } });
 await pickModel("Model B");
 expect(screen.queryByRole("slider")).not.toBeInTheDocument();
 await pickModel("Model A");
 expect(screen.getByRole("slider", { name: "Reasoning effort" })).toHaveAttribute("aria-valuetext", "High");
 fireEvent.change(screen.getByRole("slider", { name: "Reasoning effort" }), { target: { value: "0" } });
 view.unmount(); render(<HomeAgentComposer />); const start = await fill(); fireEvent.click(start);
 await waitFor(() => expect(api.start).toHaveBeenCalledWith(expect.objectContaining({ reasoning: "" })));
});
// Base UI ignores a release that follows the opening press too closely, so settle before choosing.
async function pick(name: RegExp) {
 const option = await screen.findByRole("option", { name }); await new Promise((resolve) => setTimeout(resolve, 400));
 fireEvent.pointerUp(option, { button: 0, pointerType: "mouse" }); fireEvent.mouseUp(option, { button: 0 }); fireEvent.click(option);
}
it("launches in Manual by default and remembers Auto but never Yolo", async () => {
 const modes = [{ mode: "manual", available: true, approximated: false, detail: "" }, { mode: "auto", available: true, approximated: false, detail: "Codex's auto-review decides." }, { mode: "yolo", available: true, approximated: false, detail: "" }];
 api.options.mockResolvedValue({ ...options, harnesses: [{ ...harness, permissionModes: modes }] });
 const { unmount } = render(<HomeAgentComposer />); const start = await fill();
 expect(screen.getByRole("combobox", { name: "Permission mode: Manual" })).toBeVisible();
 fireEvent.click(start);
 await waitFor(() => expect(api.start).toHaveBeenLastCalledWith(expect.objectContaining({ permissionMode: "manual", confirmYolo: false })));
 fireEvent.change(screen.getByLabelText("Task for your agent"), { target: { value: "Second task" } });
 fireEvent.click(screen.getByRole("combobox", { name: "Permission mode: Manual" }));
 await pick(/Auto/);
 fireEvent.click(screen.getByRole("button", { name: "Start task" }));
 await waitFor(() => expect(api.start).toHaveBeenLastCalledWith(expect.objectContaining({ permissionMode: "auto", confirmYolo: false })));
 expect(localStorage.getItem("orole.task.permissionMode")).toBe("auto");
 unmount(); render(<HomeAgentComposer />); await fill();
 expect(await screen.findByRole("combobox", { name: "Permission mode: Auto" })).toBeVisible();
 fireEvent.click(screen.getByRole("combobox", { name: "Permission mode: Auto" }));
 await pick(/Yolo/);
 expect(api.start).toHaveBeenCalledTimes(2);
 fireEvent.click(await screen.findByRole("button", { name: "Enable Yolo" }));
 await waitFor(() => expect(screen.getByRole("combobox", { name: "Permission mode: Yolo" })).toBeVisible());
 fireEvent.click(screen.getByRole("button", { name: "Start task" }));
 await waitFor(() => expect(api.start).toHaveBeenLastCalledWith(expect.objectContaining({ permissionMode: "yolo", confirmYolo: true })));
 expect(localStorage.getItem("orole.task.permissionMode")).toBe("auto");
});
it("offers only Manual for an agent that reports no permission modes", async () => {
 render(<HomeAgentComposer />); const start = await fill();
 fireEvent.click(screen.getByRole("combobox", { name: "Permission mode: Manual" }));
 expect(await screen.findByRole("option", { name: /Auto.*keeps its own approvals/ })).toHaveAttribute("aria-disabled", "true");
 fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" }); fireEvent.click(start);
 await waitFor(() => expect(api.start).toHaveBeenCalledWith(expect.objectContaining({ permissionMode: "manual", confirmYolo: false })));
});
