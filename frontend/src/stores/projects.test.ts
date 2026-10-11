import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { FOCUS_MIN_INTERVAL_MS } from "@/lib/focus";
import type { Project, Worktree } from "@/mygo";

const project = (id: string, over: Partial<Project> = {}): Project => ({ id, name: id, machineId: "local", path: `C:\\${id}`, remoteUrl: "", lastOpened: 0, createdAt: 0, worktreeRoot: "", worktreeRootCustom: false, missing: false, isGit: true, ...over });
const wt = (id: string, projectId: string, over: Partial<Worktree> = {}): Worktree => ({ id, projectId, name: id, path: `C:\\${id}`, branch: "main", head: "", baseRef: "", isMain: false, detached: false, locked: false, prunable: false, createdAt: 0, upstream: "", ahead: 0, behind: 0, staged: 0, unstaged: 0, untracked: 0, dirty: 0, ...over });

const mocks = vi.hoisted(() => ({
  list: vi.fn(), create: vi.fn(), remove: vi.fn(), wtList: vi.fn(), wtCreate: vi.fn(), wtRemove: vi.fn(),
  handlers: new Map<string, () => void>(),
}));

vi.mock("@/lib/backend", () => ({ inDesktop: true, previewKey: "x" }));
vi.mock("@/mygo", () => ({
  Projects: { list: mocks.list, create: mocks.create, remove: mocks.remove, open: vi.fn(), setWorktreeRoot: vi.fn() },
  Worktrees: { list: mocks.wtList, create: mocks.wtCreate, remove: mocks.wtRemove },
  events: {
    projectsChanged: { on: (f: () => void) => { mocks.handlers.set("p", f); return () => mocks.handlers.delete("p"); } },
    worktreesChanged: { on: (f: () => void) => { mocks.handlers.set("w", f); return () => mocks.handlers.delete("w"); } },
  },
}));

const { useProjects, worktreeState, WORKTREES_TTL_MS } = await import("./projects");
const releases: (() => void)[] = [];
const settle = async () => { for (let i = 0; i < 20; i++) await Promise.resolve(); };

beforeEach(() => {
  vi.clearAllMocks();
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(1_000_000);
  localStorage.clear();
  useProjects.setState({ loaded: false, loading: false, error: null, projects: [], worktrees: {}, worktreesAt: {}, worktreesError: null, expanded: {} });
});
afterEach(() => { releases.splice(0).forEach((release) => release()); vi.useRealTimers(); });

describe("projects store", () => {
  it("loads the project list", async () => {
    mocks.list.mockResolvedValue([project("a"), project("b", { isGit: false })]);
    await useProjects.getState().load();
    expect(useProjects.getState().projects.map((p) => p.id)).toEqual(["a", "b"]);
  });

  it("surfaces daemon errors instead of faking data", async () => {
    mocks.list.mockRejectedValue(new Error("daemon unavailable"));
    await useProjects.getState().load();
    expect(useProjects.getState()).toMatchObject({ loaded: true, error: "daemon unavailable", projects: [] });
  });

  it("create trims the name, passes the optional folder and reloads", async () => {
    mocks.create.mockResolvedValue(project("n", { path: "", isGit: false }));
    mocks.list.mockResolvedValue([project("n", { path: "", isGit: false })]);
    expect((await useProjects.getState().create("  Notes ", "")).id).toBe("n");
    expect(mocks.create).toHaveBeenCalledWith("Notes", "");
    expect(useProjects.getState().projects).toHaveLength(1);
    expect(mocks.wtList).not.toHaveBeenCalled();
  });

  it("create rejects with the daemon error and leaves the list alone", async () => {
    mocks.create.mockRejectedValue(new Error("bad_request: C:\\x is not a directory"));
    await expect(useProjects.getState().create("x", "C:\\x")).rejects.toThrow("is not a directory");
    expect(mocks.list).not.toHaveBeenCalled();
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

  it("startSync reloads on projects events and a throttled window focus, and cleans up", async () => {
    mocks.list.mockResolvedValue([]);
    const stop = useProjects.getState().startSync();
    await vi.waitFor(() => expect(mocks.list).toHaveBeenCalledTimes(1));
    mocks.handlers.get("p")!();
    await vi.waitFor(() => expect(mocks.list).toHaveBeenCalledTimes(2));
    vi.setSystemTime(Date.now() + FOCUS_MIN_INTERVAL_MS);
    for (let i = 0; i < 10; i++) window.dispatchEvent(new Event("focus"));
    await vi.waitFor(() => expect(mocks.list).toHaveBeenCalledTimes(3));
    await settle();
    expect(mocks.list).toHaveBeenCalledTimes(3);
    stop();
    expect(mocks.handlers.size).toBe(0);
    vi.setSystemTime(Date.now() + FOCUS_MIN_INTERVAL_MS);
    window.dispatchEvent(new Event("focus"));
    await settle();
    expect(mocks.list).toHaveBeenCalledTimes(3);
  });
});

describe("lazy worktrees", () => {
  const gitProjects = [project("a"), project("gone", { missing: true, isGit: false }), project("plain", { isGit: false }), project("named", { path: "", isGit: false })];

  it("load fetches projects only, never worktrees", async () => {
    mocks.list.mockResolvedValue(gitProjects);
    await useProjects.getState().load();
    const s = useProjects.getState();
    expect(s.projects.map((p) => p.id)).toEqual(["a", "gone", "plain", "named"]);
    expect(mocks.wtList).not.toHaveBeenCalled();
    expect(s.worktrees).toEqual({});
    expect(s.loaded && s.error === null).toBe(true);
  });

  it("loadWorktrees fetches existing git projects only and reuses lists younger than the TTL", async () => {
    useProjects.setState({ projects: gitProjects });
    mocks.wtList.mockResolvedValue([wt("w1", "a", { isMain: true })]);
    await useProjects.getState().loadWorktrees();
    expect(Object.keys(useProjects.getState().worktrees)).toEqual(["a"]);
    expect(mocks.wtList).toHaveBeenCalledTimes(1);
    await useProjects.getState().loadWorktrees();
    expect(mocks.wtList).toHaveBeenCalledTimes(1);
    vi.setSystemTime(Date.now() + WORKTREES_TTL_MS - 1);
    await useProjects.getState().loadWorktrees();
    expect(mocks.wtList).toHaveBeenCalledTimes(1);
    vi.setSystemTime(Date.now() + 1);
    await useProjects.getState().loadWorktrees();
    expect(mocks.wtList).toHaveBeenCalledTimes(2);
    await useProjects.getState().loadWorktrees(true);
    expect(mocks.wtList).toHaveBeenCalledTimes(3);
  });

  it("coalesces concurrent requests into the one in flight, plus one forced rerun", async () => {
    useProjects.setState({ projects: [project("a")] });
    let finish!: (list: Worktree[]) => void;
    mocks.wtList.mockImplementationOnce(() => new Promise<Worktree[]>((resolve) => { finish = resolve; }));
    mocks.wtList.mockResolvedValue([]);
    const first = useProjects.getState().loadWorktrees();
    const rest = [useProjects.getState().loadWorktrees(), useProjects.getState().loadWorktrees(), useProjects.getState().loadWorktrees(true), useProjects.getState().loadWorktrees(true)];
    expect(mocks.wtList).toHaveBeenCalledTimes(1);
    finish([wt("w1", "a")]);
    await Promise.all([first, ...rest]);
    expect(mocks.wtList).toHaveBeenCalledTimes(2);
  });

  it("a worktrees event burst without a watching view only marks lists stale and calls nothing", async () => {
    mocks.list.mockResolvedValue([project("a")]);
    const stop = useProjects.getState().startSync();
    await settle();
    for (let i = 0; i < 10; i++) mocks.handlers.get("w")!();
    await settle();
    expect(mocks.wtList).not.toHaveBeenCalled();
    expect(mocks.list).toHaveBeenCalledTimes(1);
    stop();
  });

  it("while a view watches, the first load fetches once, focus within the TTL does not refetch, and a burst of events refetches once", async () => {
    mocks.list.mockResolvedValue([project("a"), project("b")]);
    mocks.wtList.mockResolvedValue([]);
    const stop = useProjects.getState().startSync();
    releases.push(useProjects.getState().watchWorktrees());
    await settle();
    expect(mocks.wtList).toHaveBeenCalledTimes(2);
    vi.setSystemTime(Date.now() + FOCUS_MIN_INTERVAL_MS);
    window.dispatchEvent(new Event("focus"));
    await settle();
    expect(mocks.list).toHaveBeenCalledTimes(2);
    expect(mocks.wtList).toHaveBeenCalledTimes(2);
    mocks.wtList.mockClear();
    for (let i = 0; i < 10; i++) mocks.handlers.get("w")!();
    await settle();
    // One fetch started by the first event plus one rerun for the other nine: two lists per project, not ten.
    expect(mocks.wtList.mock.calls.map(([id]) => id).sort()).toEqual(["a", "a", "b", "b"]);
    stop();
  });

  it("releasing the last watcher stops event-driven refetching", async () => {
    mocks.list.mockResolvedValue([project("a")]);
    mocks.wtList.mockResolvedValue([]);
    const stop = useProjects.getState().startSync();
    const release = useProjects.getState().watchWorktrees();
    await settle();
    release();
    release();
    mocks.wtList.mockClear();
    mocks.handlers.get("w")!();
    await settle();
    expect(mocks.wtList).not.toHaveBeenCalled();
    stop();
  });

  it("a stale list is refetched when a view starts watching, and removal prunes the cache", async () => {
    useProjects.setState({ projects: [project("a"), project("b")], worktrees: { a: [], b: [] }, worktreesAt: { a: 1, b: 1 } });
    mocks.wtList.mockResolvedValue([]);
    releases.push(useProjects.getState().watchWorktrees());
    await settle();
    expect(mocks.wtList).toHaveBeenCalledTimes(2);
    mocks.remove.mockResolvedValue(undefined);
    await useProjects.getState().remove("a");
    expect(Object.keys(useProjects.getState().worktreesAt)).toEqual(["b"]);
  });

  it("records a worktree failure separately from the project list error", async () => {
    useProjects.setState({ projects: [project("a")] });
    mocks.wtList.mockRejectedValue(new Error("git exploded"));
    await useProjects.getState().loadWorktrees();
    expect(useProjects.getState()).toMatchObject({ worktreesError: "git exploded", error: null });
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
