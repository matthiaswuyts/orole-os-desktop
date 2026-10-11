import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Agents, type Project, type Session } from "@/mygo";
import { useAgents } from "@/stores/agents";
import { useProjects } from "@/stores/projects";
import { useUI } from "@/stores/ui";
import { SidebarProjects } from "./SidebarProjects";

const project: Project = {
  id: "notes", name: "Research", machineId: "local", path: "", remoteUrl: "", lastOpened: 0, createdAt: 0,
  worktreeRoot: "", worktreeRootCustom: false, missing: false, isGit: false,
};

beforeEach(() => {
  useAgents.setState({ sessions: {} });
  useUI.setState({ view: "home" });
  useProjects.setState({ projects: [project], available: true, loaded: true, error: null, expanded: {}, startSync: vi.fn(() => () => {}), newTask: { projectId: null, prompt: "", from: "home", seq: 0 } });
});
afterEach(cleanup);

it("offers a working start action for an empty folderless project", () => {
  render(<SidebarProjects />);
  expect(screen.getByRole("button", { name: "New task in Research" })).toBeEnabled();
  fireEvent.click(screen.getByRole("button", { name: "Start a task" }));
  expect(useProjects.getState().newTask).toMatchObject({ projectId: "notes", prompt: "", from: "home" });
  expect(useUI.getState().view).toBe("new-task");
});

it("shows managed-workspace tasks under their folderless project", () => {
  useAgents.setState({ sessions: { task: {
    id: "task", projectId: "notes", project: "C:\\managed\\notes", dir: "C:\\managed\\notes", machine: "local",
    title: "Research findings", kind: "claude", status: "done", review: "", updatedAt: "2026-10-08T00:00:00Z",
  } as Session } });
  render(<SidebarProjects />);
  expect(screen.getByRole("button", { name: /Research findings/ })).toBeInTheDocument();
  expect(screen.queryByText("Attach a folder to run tasks")).toBeNull();
});

const task = (id: string, patch: Partial<Session> = {}) => ({
  id, projectId: "notes", project: "C:\\managed\\notes", dir: "C:\\managed\\notes", machine: "local",
  title: `${id} task`, kind: "claude", status: "done", review: "", updatedAt: "2026-10-08T00:00:00Z", ...patch,
} as Session);

it("lists pinned tasks first, ahead of newer ones, with no native tooltip on the rows", () => {
  useAgents.setState({ sessions: {
    newer: task("newer", { updatedAt: "2026-10-09T00:00:00Z" }),
    pinned: task("pinned", { pinned: true }),
  } });
  render(<SidebarProjects />);
  const rows = screen.getAllByRole("button", { name: /(newer|pinned) task/ });
  expect(rows.map((row) => row.textContent)).toEqual([expect.stringContaining("pinned task"), expect.stringContaining("newer task")]);
  expect(rows.every((row) => !row.closest("li")!.querySelector("[title]"))).toBe(true);
});

it("pins a task from its right-click menu and publishes the daemon's answer", async () => {
  const setPinned = vi.spyOn(Agents, "setPinned").mockResolvedValue(task("t", { pinned: true }));
  useAgents.setState({ sessions: { t: task("t") } });
  render(<SidebarProjects />);
  fireEvent.contextMenu(screen.getByRole("button", { name: /t task/ }));
  fireEvent.click(await screen.findByRole("menuitem", { name: /Pin to top/ }));
  await waitFor(() => expect(useAgents.getState().sessions.t?.pinned).toBe(true));
  expect(setPinned).toHaveBeenCalledWith("t", true);
  setPinned.mockRestore();
});

it("deletes a running task by stopping it first, after confirmation", async () => {
  const stop = vi.spyOn(Agents, "stop").mockResolvedValue(task("t", { live: false }));
  const remove = vi.spyOn(Agents, "remove").mockResolvedValue(undefined);
  useAgents.setState({ sessions: { t: task("t", { live: true, status: "working" }) } });
  render(<SidebarProjects />);
  fireEvent.contextMenu(screen.getByRole("button", { name: /t task/ }));
  fireEvent.click(await screen.findByRole("menuitem", { name: /Delete/ }));
  expect(remove).not.toHaveBeenCalled();
  fireEvent.click(await screen.findByRole("button", { name: "Delete task" }));
  await waitFor(() => expect(remove).toHaveBeenCalledWith("t"));
  expect(stop.mock.invocationCallOrder[0]).toBeLessThan(remove.mock.invocationCallOrder[0]!);
  stop.mockRestore();
  remove.mockRestore();
});
