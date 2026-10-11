import { create } from "zustand";
import { Projects, Worktrees, events, type Project, type Worktree } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { onFocusRefresh } from "@/lib/focus";
import { useUI, type ViewId } from "@/stores/ui";

const EXPANDED_KEY = "orole.projects.expanded";

function loadExpanded(): Record<string, boolean> {
  try { return JSON.parse(localStorage.getItem(EXPANDED_KEY) ?? "{}") as Record<string, boolean>; } catch { return {}; }
}

/** A refresh requested while one is in flight: run once more afterwards so no event is lost. */
let reloadQueued = false;

/** Worktree lists cost a git call per project on the backend, so a list younger than this is reused. */
export const WORKTREES_TTL_MS = 60_000;
/** Views currently showing worktrees; only while this is above zero do events and focus refetch them. */
let worktreeWatchers = 0;
let worktreesInFlight: Promise<void> | null = null;
let worktreesQueued = false;

const msg = (e: unknown) => (e instanceof Error ? e.message : String(e));

interface State {
  /** False in the browser preview: there is no daemon, so there is no project data at all. */
  available: boolean;
  loaded: boolean;
  loading: boolean;
  error: string | null;
  projects: Project[];
  /** Filled lazily by `loadWorktrees`: empty until a view that shows worktrees asks for them. */
  worktrees: Record<string, Worktree[]>;
  /** When each project's worktrees were last fetched (ms); drives the TTL. */
  worktreesAt: Record<string, number>;
  worktreesError: string | null;
  /** Per-project collapse state; projects are expanded unless explicitly collapsed. */
  expanded: Record<string, boolean>;
  /** Project shown in the Projects view. */
  selectedId: string | null;
  select: (id: string | null) => void;
  /** Reloads the project list. Worktrees are not fetched here unless a view is watching them; see `loadWorktrees`. */
  load: () => Promise<void>;
  /** Fetches worktrees of git projects that have none yet or whose list is older than the TTL; `force` refetches all. */
  loadWorktrees: (force?: boolean) => Promise<void>;
  /** Marks the caller as showing worktrees: loads them now and keeps them fresh until the returned release is called. */
  watchWorktrees: () => () => void;
  /** Whether the New project dialog is open (one instance, mounted in App). */
  newProjectOpen: boolean;
  setNewProjectOpen: (open: boolean) => void;
  /** The New task page: the project it was opened for (null = pick one), a prompt to start from, and the view to return to. `seq` makes each opening a fresh page. */
  newTask: { projectId: string | null; prompt: string; from: ViewId; seq: number };
  openNewTask: (projectId?: string | null, prompt?: string) => void;
  closeNewTask: () => void;
  /** Creates a project from a name and an optional folder; a blank name defaults to the folder name. */
  create: (name: string, path: string) => Promise<Project>;
  remove: (id: string) => Promise<void>;
  open: (id: string) => Promise<void>;
  setWorktreeRoot: (id: string, root: string) => Promise<void>;
  createWorktree: (projectId: string, name: string, baseRef: string) => Promise<Worktree>;
  removeWorktree: (id: string, force: boolean) => Promise<void>;
  toggle: (id: string) => void;
  /** Subscribes to daemon events and refreshes on window focus. Returns the cleanup. */
  startSync: () => () => void;
}

export const useProjects = create<State>((set, get) => ({
  available: inDesktop,
  loaded: false,
  loading: false,
  error: null,
  projects: [],
  worktrees: {},
  worktreesAt: {},
  worktreesError: null,
  expanded: loadExpanded(),
  selectedId: null,
  select: (selectedId) => set({ selectedId }),
  newProjectOpen: false,
  setNewProjectOpen: (newProjectOpen) => set({ newProjectOpen }),
  newTask: { projectId: null, prompt: "", from: "home", seq: 0 },
  openNewTask: (projectId = null, prompt = "") => {
    const { view, setView } = useUI.getState();
    set((s) => ({ newTask: { projectId, prompt, from: view === "new-task" ? s.newTask.from : view, seq: s.newTask.seq + 1 } }));
    setView("new-task");
  },
  closeNewTask: () => useUI.getState().setView(get().newTask.from),

  async load() {
    if (!inDesktop) { set({ loaded: true }); return; }
    if (get().loading) { reloadQueued = true; return; }
    set({ loading: true });
    try {
      const projects = await Projects.list();
      set((s) => {
        const gone = Object.keys(s.worktrees).filter((id) => !projects.some((p) => p.id === id));
        return { projects, loaded: true, loading: false, error: null, ...(gone.length ? { worktrees: omitKeys(s.worktrees, gone), worktreesAt: omitKeys(s.worktreesAt, gone) } : {}) };
      });
    } catch (e) {
      set({ loaded: true, loading: false, error: msg(e) });
    }
    if (reloadQueued) { reloadQueued = false; await get().load(); return; }
    if (worktreeWatchers > 0) await get().loadWorktrees();
  },

  loadWorktrees(force = false) {
    if (!inDesktop) return Promise.resolve();
    if (worktreesInFlight) {
      if (force) worktreesQueued = true;
      return worktreesInFlight;
    }
    const { projects, worktreesAt } = get();
    const now = Date.now();
    const due = projects.filter((p) => !p.missing && p.isGit && (force || now - (worktreesAt[p.id] ?? 0) >= WORKTREES_TTL_MS));
    if (!due.length) return Promise.resolve();
    worktreesInFlight = (async () => {
      try {
        const lists = await Promise.all(due.map(async (p) => [p.id, await Worktrees.list(p.id)] as const));
        const at = Date.now();
        set((s) => ({
          worktrees: { ...s.worktrees, ...Object.fromEntries(lists.filter(([id]) => s.projects.some((p) => p.id === id))) },
          worktreesAt: { ...s.worktreesAt, ...Object.fromEntries(lists.map(([id]) => [id, at])) },
          worktreesError: null,
        }));
      } catch (e) {
        set({ worktreesError: msg(e) });
      } finally {
        worktreesInFlight = null;
      }
      if (worktreesQueued) { worktreesQueued = false; await get().loadWorktrees(true); }
    })();
    return worktreesInFlight;
  },

  watchWorktrees() {
    worktreeWatchers++;
    void get().loadWorktrees();
    let released = false;
    return () => {
      if (released) return;
      released = true;
      worktreeWatchers--;
    };
  },

  async create(name, path) {
    const p = await Projects.create(name.trim(), path);
    await get().load();
    return p;
  },

  async remove(id) {
    await Projects.remove(id);
    set((s) => ({ projects: s.projects.filter((p) => p.id !== id), worktrees: omitKeys(s.worktrees, [id]), worktreesAt: omitKeys(s.worktreesAt, [id]), selectedId: s.selectedId === id ? null : s.selectedId }));
  },

  async open(id) { await Projects.open(id); },

  async setWorktreeRoot(id, root) {
    const p = await Projects.setWorktreeRoot(id, root);
    set((s) => ({ projects: s.projects.map((x) => (x.id === id ? p : x)) }));
  },

  async createWorktree(projectId, name, baseRef) {
    const wt = await Worktrees.create(projectId, name, baseRef);
    set({ worktreesAt: {} });
    await get().load();
    return wt;
  },

  async removeWorktree(id, force) {
    await Worktrees.remove(id, force);
    set({ worktreesAt: {} });
    await get().load();
  },

  toggle(id) {
    const next = { ...get().expanded, [id]: get().expanded[id] === false };
    localStorage.setItem(EXPANDED_KEY, JSON.stringify(next));
    set({ expanded: next });
  },

  startSync() {
    if (!inDesktop) return () => {};
    const refresh = () => void get().load();
    refresh();
    // A worktrees event only marks the lists stale; they are refetched right away only while a view is watching them.
    const refreshWorktrees = () => {
      set({ worktreesAt: {} });
      if (worktreeWatchers > 0) void get().loadWorktrees(true);
    };
    const offs = [events.projectsChanged.on(refresh), events.worktreesChanged.on(refreshWorktrees), onFocusRefresh(refresh)];
    return () => offs.forEach((o) => o());
  },
}));

function omitKeys<T>(record: Record<string, T>, keys: readonly string[]): Record<string, T> {
  return Object.fromEntries(Object.entries(record).filter(([key]) => !keys.includes(key)));
}

/** Branch state color key for a worktree: error, dirty, ahead/behind or clean. */
export function worktreeState(w: Worktree): "error" | "dirty" | "ahead" | "behind" | "clean" {
  if (w.error) return "error";
  if (w.dirty > 0) return "dirty";
  if (w.ahead > 0 && w.behind > 0) return "behind";
  if (w.ahead > 0) return "ahead";
  if (w.behind > 0) return "behind";
  return "clean";
}
