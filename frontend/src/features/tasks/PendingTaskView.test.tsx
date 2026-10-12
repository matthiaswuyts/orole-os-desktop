import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { HarnessOption, LaunchOptions, Machine, Project, Session } from "@/mygo";

const api = vi.hoisted(() => ({ options: vi.fn(), start: vi.fn(), upload: vi.fn(), discard: vi.fn(), toast: vi.fn() }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({ Agents: { options: api.options, start: api.start, uploadAttachment: api.upload, discardAttachment: api.discard }, events: {} }));
vi.mock("@/components/ui/toast", () => ({ toastManager: { add: api.toast } }));
import { HomeAgentComposer } from "@/features/home/HomeAgentComposer";
import { useAgents } from "@/stores/agents";
import { useBoxes } from "@/stores/boxes";
import { useDaemon } from "@/stores/daemon";
import { useHarnessOptionsCache } from "@/stores/harnessOptions";
import { useProjects } from "@/stores/projects";
import { useUI } from "@/stores/ui";
import { PendingTaskView } from "./PendingTaskView";

const project: Project = { id: "notes", name: "Research", machineId: "local", path: "C:\\repo", remoteUrl: "", lastOpened: 0, createdAt: 0, worktreeRoot: "", worktreeRootCustom: false, missing: false, isGit: true };
const harness: HarnessOption = { id: "codex", name: "Codex", available: true, executable: "codex", version: "1", reasoning: [], customModel: true, modes: ["structured"], structured: true, interactive: false, unavailableReason: "", permissionModes: [], models: [{ id: "model-a", name: "Model A", reasoning: [], default: true }] };
const options: LaunchOptions = { machine: "local", harnesses: [harness], projects: [], directories: [], worktree: true, unavailableReason: "" };
const session = (id: string): Session => ({ id, projectId: "notes", project: "C:\\repo", dir: "C:\\repo", machine: "local", title: "Plan this research", kind: "codex", status: "working", review: "", updatedAt: "2026-10-09T00:00:00Z" }) as Session;

/** Just enough of the app's routing: the launcher on Home, the pending page (or the opened task) in Tasks. */
function Host() {
  const view = useUI((s) => s.view);
  const openId = useAgents((s) => s.openId);
  const launch = useAgents((s) => (s.openId ? s.launches[s.openId] : undefined));
  if (view !== "agents") return <HomeAgentComposer />;
  return launch ? <PendingTaskView key={launch.id} launch={launch} /> : <p>Task {openId}</p>;
}

beforeEach(() => {
  vi.clearAllMocks(); localStorage.clear();
  useDaemon.setState({ link: "connected", connectEpoch: 0 }); useHarnessOptionsCache.setState({ machines: {} });
  api.options.mockResolvedValue(options); api.start.mockReset(); api.start.mockResolvedValue(session("task"));
  api.upload.mockResolvedValue({ id: "upload-one", name: "notes.txt", size: 8, mime: "text/plain" }); api.discard.mockResolvedValue(undefined);
  useBoxes.setState({ machines: [{ id: "local", name: "devl" }] as Machine[], observations: {} });
  useProjects.setState({ projects: [project], worktrees: {}, selectedId: null, available: true, loaded: true, newProjectOpen: false });
  useAgents.setState({ sessions: {}, launches: {}, launchDraft: null, openId: null });
  useUI.setState({ view: "home" });
});
afterEach(cleanup);

async function fill(text = "Plan this research") {
  fireEvent.change(screen.getByLabelText("Task for your agent"), { target: { value: text } });
  const start = screen.getByRole("button", { name: "Start task" });
  await waitFor(() => expect(start).toBeEnabled());
  return start;
}

it("shows the task page before the daemon answers: the message, the workspace being prepared and a disabled composer", async () => {
  const started = Promise.withResolvers<Session>(); api.start.mockReturnValue(started.promise);
  render(<Host />); const start = await fill("Plan this research\nwith a second line");
  fireEvent.click(screen.getByRole("button", { name: /^Worktree:/ })); fireEvent.click(await screen.findByRole("menuitemradio", { name: /New worktree/ }));
  fireEvent.click(start);
  expect(useUI.getState().view).toBe("agents");
  expect(screen.getByRole("log", { name: "Task conversation" })).toHaveTextContent("Plan this research");
  expect(screen.getByText(/with a second line/, { selector: "p.selectable" })).toBeInTheDocument();
  expect(screen.getByRole("status")).toHaveTextContent("Preparing workspace…");
  expect(screen.getByLabelText("Message the agent")).toBeDisabled();
  expect(Object.values(useAgents.getState().launches)[0]).toMatchObject({ title: "Plan this research", status: "starting", worktree: true });
  expect(api.start).toHaveBeenCalledTimes(1);
  started.resolve(session("task"));
  await waitFor(() => expect(screen.getByText("Task task")).toBeInTheDocument());
  expect(useAgents.getState().launches).toEqual({}); expect(useAgents.getState().sessions.task).toBeDefined();
});

it("says which agent is starting when there is no workspace to prepare", async () => {
  useProjects.setState({ projects: [{ ...project, isGit: false }] });
  api.start.mockReturnValue(Promise.withResolvers<Session>().promise);
  render(<Host />); fireEvent.click(await fill());
  expect(screen.getByRole("status")).toHaveTextContent("Starting Codex…");
});

it("shows an attachment as a chip on the pending page", async () => {
  api.start.mockReturnValue(Promise.withResolvers<Session>().promise);
  render(<Host />); await fill();
  fireEvent.change(screen.getByLabelText("Attach files"), { target: { files: [new File(["research"], "notes.txt", { type: "text/plain" })] } });
  await waitFor(() => expect(screen.getByText("8 B")).toBeInTheDocument());
  fireEvent.click(screen.getByRole("button", { name: "Start task" }));
  expect(screen.getByText("notes.txt")).toBeInTheDocument();
  expect(api.start).toHaveBeenCalledWith(expect.objectContaining({ attachmentIds: ["upload-one"] }));
});

it("stays on the page with the error when the start fails, and Try again sends the same request key", async () => {
  api.start.mockRejectedValueOnce(new Error("internal: the daemon dropped the call"));
  render(<Host />); fireEvent.click(await fill());
  expect(await screen.findByRole("alert")).toHaveTextContent("the daemon dropped the call");
  expect(screen.getByText("Plan this research", { selector: "p.selectable" })).toBeInTheDocument();
  expect(useUI.getState().view).toBe("agents");
  fireEvent.click(screen.getByRole("button", { name: "Try again" }));
  expect(screen.getByRole("status")).toHaveTextContent("Starting Codex…");
  await waitFor(() => expect(screen.getByText("Task task")).toBeInTheDocument());
  expect(api.start).toHaveBeenCalledTimes(2);
  expect(api.start.mock.calls[1]![0]).toEqual(api.start.mock.calls[0]![0]);
  expect(api.toast).not.toHaveBeenCalled();
});

it("Edit prompt returns to the launcher with the prompt, files, agent and choices restored", async () => {
  api.start.mockRejectedValueOnce(new Error("connection lost"));
  render(<Host />); await fill("Fix the flaky test");
  fireEvent.change(screen.getByLabelText("Attach files"), { target: { files: [new File(["research"], "notes.txt", { type: "text/plain" })] } });
  await waitFor(() => expect(screen.getByText("8 B")).toBeInTheDocument());
  fireEvent.click(screen.getByRole("button", { name: "Start task" }));
  fireEvent.click(await screen.findByRole("button", { name: "Edit prompt" }));
  expect(useUI.getState().view).toBe("home");
  expect(screen.getByLabelText("Task for your agent")).toHaveValue("Fix the flaky test");
  expect(screen.getByRole("button", { name: "Remove notes.txt" })).toBeEnabled();
  expect(useAgents.getState().launches).toEqual({}); expect(useAgents.getState().launchDraft).toBeNull();
  expect(api.discard).not.toHaveBeenCalled();
  const start = screen.getByRole("button", { name: "Start task" });
  await waitFor(() => expect(start).toBeEnabled()); fireEvent.click(start);
  await waitFor(() => expect(screen.getByText("Task task")).toBeInTheDocument());
  expect(api.start.mock.calls[1]![0].key).toBe(api.start.mock.calls[0]![0].key);
  expect(api.start.mock.calls[1]![0].attachmentIds).toEqual(["upload-one"]);
});

it("keeps starting after the person leaves the page, then lists the task without opening it", async () => {
  const started = Promise.withResolvers<Session>(); api.start.mockReturnValue(started.promise);
  render(<Host />); fireEvent.click(await fill());
  useAgents.getState().setOpenId(null); useUI.getState().setView("home");
  expect(Object.keys(useAgents.getState().launches)).toHaveLength(1);
  started.resolve(session("task"));
  await waitFor(() => expect(useAgents.getState().sessions.task).toBeDefined());
  expect(useAgents.getState().launches).toEqual({});
  expect(useAgents.getState().openId).toBeNull(); expect(useUI.getState().view).toBe("home");
  expect(api.start).toHaveBeenCalledTimes(1);
});

it("reports a start that fails off-page with a way back to it", async () => {
  const started = Promise.withResolvers<Session>(); api.start.mockReturnValue(started.promise);
  render(<Host />); fireEvent.click(await fill());
  useUI.getState().setView("home");
  started.reject(new Error("no space left"));
  await waitFor(() => expect(api.toast).toHaveBeenCalledWith(expect.objectContaining({ type: "error", description: "no space left" })));
  expect(Object.values(useAgents.getState().launches)[0]).toMatchObject({ status: "failed", error: "no space left" });
});

it("opens the started task only on the launch's own page when a second launch is pending", async () => {
  const first = Promise.withResolvers<Session>(); const second = Promise.withResolvers<Session>();
  api.start.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
  const view = render(<Host />); fireEvent.click(await fill("First"));
  useUI.getState().setView("home"); view.rerender(<Host />);
  fireEvent.click(await fill("Second"));
  const [one, two] = Object.values(useAgents.getState().launches);
  expect(useAgents.getState().openId).toBe(two!.id);
  first.resolve(session("first"));
  await waitFor(() => expect(useAgents.getState().sessions.first).toBeDefined());
  expect(useAgents.getState().launches[one!.id]).toBeUndefined();
  expect(useAgents.getState().launches[two!.id]).toBeDefined();
  expect(useAgents.getState().openId).toBe(two!.id);
  second.resolve(session("second"));
  await waitFor(() => expect(useAgents.getState().openId).toBe("second"));
});
