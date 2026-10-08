import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { Project, Session } from "@/mygo";
import { useAgents } from "@/stores/agents";
import { useProjects } from "@/stores/projects";
import { SidebarProjects } from "./SidebarProjects";

const project: Project = {
  id: "notes", name: "Research", machineId: "local", path: "", remoteUrl: "", lastOpened: 0, createdAt: 0,
  worktreeRoot: "", worktreeRootCustom: false, missing: false, isGit: false,
};

beforeEach(() => {
  useAgents.setState({ sessions: {} });
  useProjects.setState({ projects: [project], available: true, loaded: true, error: null, expanded: {}, startSync: vi.fn(() => () => {}), newTask: { open: false, projectId: null, prompt: "" } });
});
afterEach(cleanup);

it("offers a working start action for an empty folderless project", () => {
  render(<SidebarProjects />);
  expect(screen.getByRole("button", { name: "New task in Research" })).toBeEnabled();
  fireEvent.click(screen.getByRole("button", { name: "Start a task" }));
  expect(useProjects.getState().newTask).toEqual({ open: true, projectId: "notes", prompt: "" });
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
