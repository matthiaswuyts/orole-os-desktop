import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Project, Worktree } from "@/mygo";

const project = (id: string, over: Partial<Project> = {}): Project => ({ id, name: id, machineId: "local", path: `C:\\${id}`, remoteUrl: "", lastOpened: 0, createdAt: 0, worktreeRoot: "", worktreeRootCustom: false, missing: false, ...over });
const wt = (id: string, projectId: string, over: Partial<Worktree> = {}): Worktree => ({ id, projectId, name: id, path: `C:\\${id}`, branch: "main", head: "", baseRef: "", isMain: false, detached: false, locked: false, prunable: false, createdAt: 0, upstream: "", ahead: 0, behind: 0, staged: 0, unstaged: 0, untracked: 0, dirty: 0, ...over });

const mocks = vi.hoisted(() => ({
  list: vi.fn(), add: vi.fn(), remove: vi.fn(), pick: vi.fn(), wtList: vi.fn(), wtCreate: vi.fn(), wtRemove: vi.fn(),
  handlers: new Map<string, () => void>(),
}));

vi.mock("@/lib/backend", () => ({ inDesktop: true, previewKey: "x" }));
vi.mock("@/mygo", () => ({
  Projects: { list: mocks.list, add: mocks.add, remove: mocks.remove, pickFolder: mocks.pick, open: vi.fn(), setWorktreeRoot: vi.fn() },
  Worktrees: { list: mocks.wtList, create: mocks.wtCreate, remove: mocks.wtRemove },
  events: {
    projectsChanged: { on: (f: () => void) => { mocks.handlers.set("p", f); return () => mocks.handlers.delete("p"); } },
    worktreesChanged: { on: (f: () => void) => { mocks.handlers.set("w", f); return () => mocks.handlers.delete("w"); } },
  },
}));

const { useProjects, worktreeState } = await import("./projects");

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  useProjects.setState({ loaded: false, loading: false, error: null, projects: [], worktrees: {}, expanded: {} });
});

describe("projects store", () => {
  it("loads projects and their worktrees, skipping missing directories", async () => {
    mocks.list.mockResolvedValue([project("a"), project("gone", { missing: true })]);
    mocks.wtList.mockResolvedValue([wt("w1", "a", { isMain: true })]);
    await useProjects.getState().load();
    const s = useProjects.getState();
    expect(s.projects.map((p) => p.id)).toEqual(["a", "gone"]);
    expect(Object.keys(s.worktrees)).toEqual(["a"]);
    expect(mocks.wtList).toHaveBeenCalledTimes(1);
    expect(s.loaded && s.error === null).toBe(true);
  });

  it("surfaces daemon errors instead of faking data", async () => {
    mocks.list.mockRejectedValue(new Error("daemon unavailable"));
    await useProjects.getState().load();
    expect(useProjects.getState()).toMatchObject({ loaded: true, error: "daemon unavailable", projects: [] });
  });

  it("pickAndAdd does nothing when the picker is cancelled and adds otherwise", async () => {
    mocks.pick.mockResolvedValueOnce("");
    expect(await useProjects.getState().pickAndAdd()).toBeNull();
    expect(mocks.add).not.toHaveBeenCalled();
    mocks.pick.mockResolvedValueOnce("C:\\repo");
    mocks.add.mockResolvedValue(project("r"));
    mocks.list.mockResolvedValue([project("r")]);
    mocks.wtList.mockResolvedValue([]);
    expect((await useProjects.getState().pickAndAdd())?.id).toBe("r");
    expect(mocks.add).toHaveBeenCalledWith("C:\\repo");
    expect(useProjects.getState().projects).toHaveLength(1);
  });

  it("remove drops the project and its worktrees locally after the daemon confirms", async () => {
    useProjects.setState({ projects: [project("a"), project("b")], worktrees: { a: [wt("w", "a")], b: [] } });
    mocks.remove.mockResolvedValue(undefined);
    await useProjects.getState().remove("a");
    const s = useProjects.getState();
    expect(s.projects.map((p) => p.id)).toEqual(["b"]);
    expect(Object.keys(s.worktrees)).toEqual(["b"]);
  });

  it("keeps the project when the daemon refuses removal", async () => {
    useProjects.setState({ projects: [project("a")] });
    mocks.remove.mockRejectedValue(new Error("nope"));
    await expect(useProjects.getState().remove("a")).rejects.toThrow("nope");
    expect(useProjects.getState().projects).toHaveLength(1);
  });

  it("toggle persists collapse state and defaults to expanded", () => {
    expect(useProjects.getState().expanded.a).toBeUndefined();
    useProjects.getState().toggle("a");
    expect(useProjects.getState().expanded.a).toBe(false);
    expect(JSON.parse(localStorage.getItem("orole.projects.expanded")!)).toEqual({ a: false });
    useProjects.getState().toggle("a");
    expect(useProjects.getState().expanded.a).toBe(true);
  });

  it("startSync reloads on daemon events and window focus, and cleans up", async () => {
    mocks.list.mockResolvedValue([]);
    const stop = useProjects.getState().startSync();
    await vi.waitFor(() => expect(mocks.list).toHaveBeenCalledTimes(1));
    mocks.handlers.get("w")!();
    await vi.waitFor(() => expect(mocks.list).toHaveBeenCalledTimes(2));
    window.dispatchEvent(new Event("focus"));
    await vi.waitFor(() => expect(mocks.list).toHaveBeenCalledTimes(3));
    stop();
    expect(mocks.handlers.size).toBe(0);
    window.dispatchEvent(new Event("focus"));
    await new Promise((r) => setTimeout(r, 20));
    expect(mocks.list).toHaveBeenCalledTimes(3);
  });
});

describe("worktreeState", () => {
  it("ranks error > dirty > ahead/behind > clean", () => {
    expect(worktreeState(wt("a", "p", { error: "x", dirty: 3 }))).toBe("error");
    expect(worktreeState(wt("a", "p", { dirty: 1, ahead: 2 }))).toBe("dirty");
    expect(worktreeState(wt("a", "p", { ahead: 2 }))).toBe("ahead");
    expect(worktreeState(wt("a", "p", { behind: 1 }))).toBe("behind");
    expect(worktreeState(wt("a", "p"))).toBe("clean");
  });
});
