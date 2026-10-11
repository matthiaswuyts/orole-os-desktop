import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { LaunchOptions, Project, Session } from "@/mygo";

const api = vi.hoisted(() => ({ options: vi.fn(), start: vi.fn(), toast: vi.fn() }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({ Agents: { options: api.options, start: api.start }, events: {} }));
vi.mock("./dialogs", async (original) => ({ ...(await original<object>()), toastError: api.toast }));
vi.mock("@/features/agents/ProviderLogo", () => ({ ProviderLogo: () => null }));
vi.mock("@/components/ascii/AsciiMark", () => ({ AsciiMark: () => null }));
import { NewTaskView } from "./NewTaskView";
import { editLaunch } from "@/features/tasks/pendingLaunch";
import { useAgents } from "@/stores/agents";
import { useBoxes } from "@/stores/boxes";
import { useDaemon } from "@/stores/daemon";
import { useHarnessOptionsCache } from "@/stores/harnessOptions";
import { useProjects } from "@/stores/projects";
import { useUI } from "@/stores/ui";

const project = (over: Partial<Project>): Project => ({
  id: "p1", name: "alpha", machineId: "local", path: "C:\\code\\alpha", remoteUrl: "https://github.com/me/alpha.git", lastOpened: 0, createdAt: 0,
  worktreeRoot: "", worktreeRootCustom: false, missing: false, isGit: true, ...over,
});
const harness = (id: string, name: string) => ({
  id, name, available: true, executable: id, version: "1", reasoning: [], customModel: true, modes: ["structured", "process"], structured: true, interactive: false, unavailableReason: "", permissionModes: [], models: [{ id: `${id}-default`, name: `${name} default`, reasoning: [], default: true }],
}) as LaunchOptions["harnesses"][number];
const options: LaunchOptions = { machine: "local", projects: [], directories: [], worktree: true, unavailableReason: "", harnesses: [harness("claude", "Claude Code"), harness("codex", "Codex")] };
const session = (id: string, over: Partial<Session>): Session => ({ id, projectId: "p1", project: "C:\\code\\alpha", dir: "C:\\code\\alpha", machine: "local", title: id, kind: "claude", status: "done", review: "", updatedAt: "2026-10-09T00:00:00Z", ...over }) as Session;

beforeEach(() => {
  vi.clearAllMocks(); localStorage.clear();
  api.options.mockResolvedValue(options);
  api.start.mockResolvedValue(session("s1", {}));
  useDaemon.setState({ link: "connected", connectEpoch: 0 }); useHarnessOptionsCache.setState({ machines: {} });
  useAgents.setState({ sessions: {} });
  useBoxes.setState({ machines: [], observations: {} });
  useUI.setState({ view: "home" });
  useAgents.setState({ sessions: {}, launches: {}, launchDraft: null, openId: null });
  useProjects.setState({ projects: [project({}), project({ id: "p2", name: "notes", path: "", isGit: false, remoteUrl: "" })], worktrees: {}, loaded: true, selectedId: "p2", newTask: { projectId: null, prompt: "", from: "home", seq: 0 } });
});
afterEach(cleanup);

const open = (id: string | null, prompt = "") => { useProjects.getState().openNewTask(id, prompt); return render(<NewTaskView />); };
async function ready() {
  const start = screen.getByRole("button", { name: "Start task" });
  await waitFor(() => expect(start).toBeEnabled());
  return start;
}

it("is a page for the project it was opened from, not the project Home last used", async () => {
  open("p1", "Fix the login redirect");
  expect(useUI.getState().view).toBe("new-task");
  expect(screen.getByRole("heading", { name: "alpha" })).toBeInTheDocument();
  expect(screen.getByText("me/alpha")).toBeInTheDocument();
  expect(screen.getByText("C:\\code\\alpha")).toBeInTheDocument();
  expect(screen.getByPlaceholderText("What should an agent do in alpha?")).toBeInTheDocument();
  const started = Promise.withResolvers<Session>(); api.start.mockReturnValue(started.promise);
  fireEvent.click(await ready());
  expect(useUI.getState().view).toBe("agents");
  expect(Object.values(useAgents.getState().launches)[0]).toMatchObject({ status: "starting", title: "Fix the login redirect", projectId: "p1", worktree: true });
  started.resolve(session("s1", {}));
  await waitFor(() => expect(useAgents.getState().openId).toBe("s1"));
  expect(api.start).toHaveBeenCalledWith(expect.objectContaining({ projectId: "p1", prompt: "Fix the login redirect", machine: "local", mode: "structured", newWorktree: true }));
});

it("starts on the remote system of a remote project", async () => {
  useProjects.setState({ projects: [project({ machineId: "remote" })] });
  open("p1", "Run remote checks");
  fireEvent.click(await ready());
  await waitFor(() => expect(api.start).toHaveBeenCalledWith(expect.objectContaining({ projectId: "p1", machine: "remote", mode: "process" })));
  expect(api.options).toHaveBeenCalledWith("remote");
});

it("fails on the pending page, then Edit prompt returns the draft and an identical resend reuses the request key", async () => {
  api.start.mockRejectedValueOnce(new Error("connection lost"));
  open("p1", "Run the tests");
  fireEvent.click(await ready());
  await waitFor(() => expect(Object.values(useAgents.getState().launches)[0]?.status).toBe("failed"));
  const [failed] = Object.values(useAgents.getState().launches);
  expect(failed!.error).toBe("connection lost");
  act(() => editLaunch(failed!.id));
  expect(useAgents.getState().launches).toEqual({});
  expect(useUI.getState().view).toBe("new-task");
  expect(screen.getByLabelText("Task for your agent")).toHaveValue("Run the tests");
  fireEvent.click(await ready());
  await waitFor(() => expect(useAgents.getState().openId).toBe("s1"));
  expect(api.start.mock.calls[1]![0].key).toBe(api.start.mock.calls[0]![0].key);
});

it("a quick action picks the agent, focuses the input and is what the task starts with", async () => {
  open("p1", "Plan the migration");
  const quick = within(screen.getByRole("region", { name: "Quick actions" }));
  fireEvent.click(await quick.findByRole("button", { name: /^New Codex/ }));
  expect(quick.getByRole("button", { name: /^New Codex/ })).toHaveAttribute("aria-pressed", "true");
  expect(quick.getByRole("button", { name: /^New Claude Code/ })).toHaveAttribute("aria-pressed", "false");
  expect(screen.getByLabelText("Task for your agent")).toHaveFocus();
  fireEvent.click(await ready());
  await waitFor(() => expect(api.start).toHaveBeenCalledWith(expect.objectContaining({ kind: "codex" })));
});

it("lists only this project's recent tasks and opens one", async () => {
  useAgents.setState({ sessions: {
    mine: session("mine", { title: "Fix checkout webhook retries", status: "needs-you" }),
    other: session("other", { projectId: "p2", project: "C:\\managed\\notes", title: "Unrelated research" }),
    gone: session("gone", { title: "Discarded work", review: "discarded" }),
  } });
  open("p1");
  const recent = within(screen.getByRole("region", { name: "Pick up where you left off" }));
  expect(recent.getByText("Fix checkout webhook retries")).toBeInTheDocument();
  expect(recent.getByText(/needs you/)).toBeInTheDocument();
  expect(recent.queryByText("Unrelated research")).toBeNull();
  expect(recent.queryByText("Discarded work")).toBeNull();
  fireEvent.click(recent.getByRole("button", { name: "Open" }));
  expect(useAgents.getState().openId).toBe("mine"); expect(useUI.getState().view).toBe("agents");
});

it("has no recent-tasks section for a project with none", () => {
  open("p1");
  expect(screen.queryByRole("region", { name: "Pick up where you left off" })).toBeNull();
});

it("returns to the view it was opened from and starts a fresh page each time", async () => {
  useUI.setState({ view: "issues" });
  const view = open("p1", "First draft");
  useProjects.getState().closeNewTask();
  expect(useUI.getState().view).toBe("issues");
  view.unmount();
  open("p2", "Second draft");
  expect(screen.getByRole("heading", { name: "notes" })).toBeInTheDocument();
  expect(screen.getByLabelText("Task for your agent")).toHaveValue("Second draft");
  // Opening again from the page itself keeps the original return target.
  useProjects.getState().openNewTask("p1");
  expect(useProjects.getState().newTask.from).toBe("issues");
});

it("does not offer a project menu once the page has a project, but does when there is none to start from", () => {
  open("p1");
  expect(screen.queryByRole("button", { name: "Project" })).toBeNull();
  cleanup();
  useProjects.setState({ projects: [] });
  open(null);
  expect(screen.getByRole("button", { name: "Project" })).toBeInTheDocument();
});

it("marks the repository with GitHub, the host with its operating system and the folder with a folder icon", () => {
  useBoxes.setState({ machines: [{ id: "local", name: "devl", os: "linux" }] as never, observations: {} });
  open("p1");
  const badges = screen.getByRole("heading", { name: "alpha" }).parentElement!;
  expect(within(badges).getByText("me/alpha").parentElement!.querySelector("[role=img]")).toHaveAccessibleName("GitHub");
  expect(within(badges).getByText("devl").parentElement!.querySelector("[role=img]")).toHaveAccessibleName("Linux");
  expect(within(badges).getByText("C:\\code\\alpha").parentElement!.querySelector("svg")).not.toBeNull();
});

it("shows the live operating system of a remote host over its registered one", () => {
  useProjects.setState({ projects: [project({ machineId: "box" })] });
  useBoxes.setState({ machines: [{ id: "box", name: "gpu", os: "linux" }] as never, observations: { box: { health: { os: "windows" } as never, error: "", attemptedAt: Date.now() } } });
  open("p1");
  expect(screen.getByRole("img", { name: "Windows" })).toBeInTheDocument();
  expect(screen.queryByRole("img", { name: "Linux" })).toBeNull();
});
