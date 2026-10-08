import type { ReactNode } from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { LaunchOptions, Project } from "@/mygo";

const api = vi.hoisted(() => ({ options: vi.fn(), start: vi.fn(), toast: vi.fn(), openTask: vi.fn() }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({ Agents: { options: api.options, start: api.start } }));
vi.mock("@/stores/daemon", () => ({ useDaemon: (select: (s: unknown) => unknown) => select({ link: "connected" }) }));
vi.mock("./dialogs", () => ({ toastError: api.toast }));
vi.mock("./tasks", () => ({ openTask: api.openTask }));
vi.mock("@/features/agents/ProviderLogo", () => ({ ProviderLogo: () => null }));
vi.mock("@/components/ui/dialog", () => {
  const Box = ({ children }: { children: ReactNode }) => <div>{children}</div>;
  return { Dialog: ({ open, children }: { open: boolean; children: ReactNode }) => (open ? <div>{children}</div> : null), DialogPopup: Box, DialogHeader: Box, DialogPanel: Box, DialogFooter: Box, DialogTitle: Box, DialogDescription: Box };
});
import { NewTaskDialog } from "./NewTaskDialog";
import { useAgents } from "@/stores/agents";
import { useProjects } from "@/stores/projects";

const harness = (id: string, over: Partial<LaunchOptions["harnesses"][number]> = {}) => ({
  id, name: id, available: true, executable: id, version: "1.0", reasoning: [], customModel: true, modes: ["structured"], structured: true, interactive: false, unavailableReason: "", ...over,
}) as LaunchOptions["harnesses"][number];
const project = (over: Partial<Project>): Project => ({
  id: "p1", name: "alpha", machineId: "local", path: "C:\\code\\alpha", remoteUrl: "", lastOpened: 0, createdAt: 0,
  worktreeRoot: "", worktreeRootCustom: false, missing: false, isGit: true, ...over,
});
const options = (over: Partial<LaunchOptions> = {}): LaunchOptions => ({
  machine: "local", harnesses: [harness("omp"), harness("claude", { available: false, unavailableReason: "Not installed" }), harness("codex")],
  projects: [], directories: [], worktree: true, unavailableReason: "", ...over,
});

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  api.options.mockResolvedValue(options());
  useAgents.setState({ sessions: {} });
  useProjects.setState({ projects: [project({}), project({ id: "p2", name: "notes", path: "" })], worktrees: {}, newTask: { open: false, projectId: null, prompt: "" } });
});
afterEach(cleanup);

it("starts an isolated task on the project folder with an idempotency key and opens it", async () => {
  api.start.mockResolvedValue({ id: "s1", updatedAt: "2026-01-01T00:00:00Z" });
  useProjects.getState().openNewTask("p1", "Fix the login redirect");
  render(<NewTaskDialog />);
  const start = await screen.findByRole("button", { name: "Start task" });
  await waitFor(() => expect(start).toBeEnabled());
  expect(screen.getByLabelText("Branch name")).toHaveValue("orole/fix-the-login-redirect");
  fireEvent.click(start);
  await waitFor(() => expect(api.openTask).toHaveBeenCalledWith("s1"));
  expect(api.options).toHaveBeenCalledWith("local");
  expect(api.start).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({
    kind: "omp", machine: "local", projectId: "p1", dir: "", prompt: "Fix the login redirect", mode: "structured",
    newWorktree: true, branch: "orole/fix-the-login-redirect", baseRef: "", key: expect.any(String),
  }));
  expect(api.start.mock.calls[0]![0].key).not.toBe("");
  expect(useProjects.getState().newTask.open).toBe(false);
  expect(useAgents.getState().sessions.s1).toBeDefined();
});

it("works in place for a project that is not a git repository", async () => {
  useProjects.setState({ projects: [project({ isGit: false })] });
  api.start.mockResolvedValue({ id: "s2", updatedAt: "2026-01-01T00:00:00Z" });
  useProjects.getState().openNewTask("p1", "Summarize the folder");
  render(<NewTaskDialog />);
  const start = await screen.findByRole("button", { name: "Start task" });
  await waitFor(() => expect(start).toBeEnabled());
  expect(screen.queryByLabelText("Branch name")).toBeNull();
  fireEvent.click(start);
  await waitFor(() => expect(api.start).toHaveBeenCalled());
  expect(api.start).toHaveBeenCalledWith(expect.objectContaining({ newWorktree: false, branch: "", projectId: "p1", dir: "" }));
});

it("reports a failed start with a toast and keeps the dialog open for a retry with the same key", async () => {
  api.start.mockRejectedValueOnce(new Error("agent limit reached")).mockResolvedValueOnce({ id: "s3", updatedAt: "2026-01-01T00:00:00Z" });
  useProjects.getState().openNewTask("p1", "Run the tests");
  render(<NewTaskDialog />);
  const start = await screen.findByRole("button", { name: "Start task" });
  await waitFor(() => expect(start).toBeEnabled());
  fireEvent.click(start);
  await waitFor(() => expect(api.toast).toHaveBeenCalledWith("Could not start task", expect.any(Error)));
  expect(useProjects.getState().newTask.open).toBe(true);
  await waitFor(() => expect(start).toBeEnabled());
  fireEvent.click(start);
  await waitFor(() => expect(api.openTask).toHaveBeenCalledWith("s3"));
  expect(api.start.mock.calls[1]![0].key).toBe(api.start.mock.calls[0]![0].key);
});

it("starts a folderless task by stable project ID without requiring a user folder", async () => {
  useProjects.setState({ projects: [project({ id: "p2", name: "notes", path: "", isGit: false })] });
  api.start.mockResolvedValue({ id: "folderless", projectId: "p2", updatedAt: "2026-01-01T00:00:00Z" });
  useProjects.getState().openNewTask("p2", "Plan the research");
  render(<NewTaskDialog />);
  const start = await screen.findByRole("button", { name: "Start task" });
  await waitFor(() => expect(start).toBeEnabled());
  expect(screen.getByText("No folder needed. Orole keeps this project's files in a managed workspace.")).toBeInTheDocument();
  expect(screen.queryByLabelText("Branch name")).toBeNull();
  fireEvent.click(start);
  await waitFor(() => expect(api.openTask).toHaveBeenCalledWith("folderless"));
  expect(api.start).toHaveBeenCalledWith(expect.objectContaining({ projectId: "p2", dir: "", newWorktree: false, branch: "" }));
});

it("does not offer projects whose attached folders are missing", async () => {
  useProjects.setState({ projects: [project({ missing: true })] });
  useProjects.getState().openNewTask("p1", "x");
  render(<NewTaskDialog />);
  expect(await screen.findByText("Add a project to run tasks.")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Start task" })).toBeDisabled();
});

it("uses the discovered default model rather than a stale configured or recent model", async () => {
  api.options.mockResolvedValue(options({ harnesses: [harness("codex", { models: [
    { id: "old", name: "Old model", reasoning: ["low"], default: false },
    { id: "available", name: "Available model", reasoning: ["high"], default: true },
  ] })] }));
  api.start.mockResolvedValue({ id: "models", updatedAt: "2026-01-01T00:00:00Z" });
  useProjects.getState().openNewTask("p1", "Use the available model");
  render(<NewTaskDialog />);
  await screen.findByPlaceholderText("Available model (default)");
  expect(document.querySelector('datalist option[value="available"]')).not.toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Start task" }));
  await waitFor(() => expect(api.start).toHaveBeenCalledWith(expect.objectContaining({ model: "available", reasoning: "", mode: "structured" })));
});

it("shows reasoning only when supported by the chosen model", async () => {
  api.options.mockResolvedValue(options({ harnesses: [harness("codex", { reasoning: ["low", "high"], models: [
    { id: "thinking", name: "Thinking", reasoning: ["high"], default: true },
    { id: "fast", name: "Fast", reasoning: [], default: false },
  ] })] }));
  useProjects.getState().openNewTask("p1", "Choose a model");
  render(<NewTaskDialog />);
  const model = await screen.findByPlaceholderText("Thinking (default)");
  expect(screen.getByLabelText("Reasoning")).toBeInTheDocument();
  fireEvent.change(model, { target: { value: "fast" } });
  expect(screen.queryByLabelText("Reasoning")).toBeNull();
  fireEvent.change(model, { target: { value: "" } });
  expect(screen.getByLabelText("Reasoning")).toBeInTheDocument();
});

it("does not offer terminal-only harnesses or terminal mode for new tasks", async () => {
  api.options.mockResolvedValue(options({ harnesses: [
    harness("codex", { modes: ["structured", "terminal"] }),
    harness("custom", { modes: ["terminal"], structured: false }),
  ] }));
  useProjects.getState().openNewTask("p1", "Structured task");
  render(<NewTaskDialog />);
  await screen.findByRole("radio", { name: "codex" });
  expect(screen.queryByRole("radio", { name: "custom" })).toBeNull();
  expect(screen.queryByLabelText("Run mode")).toBeNull();
  expect(screen.queryByLabelText("Command line")).toBeNull();
});
