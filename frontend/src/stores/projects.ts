import { create } from "zustand";
import { Projects, Worktrees, events, type Project, type Worktree } from "@/mygo";
import { inDesktop } from "@/lib/backend";

const EXPANDED_KEY = "orole.projects.expanded";

function loadExpanded(): Record<string, boolean> {
  try { return JSON.parse(localStorage.getItem(EXPANDED_KEY) ?? "{}") as Record<string, boolean>; } catch { return {}; }
}

/** A refresh requested while one is in flight: run once more afterwards so no event is lost. */
let reloadQueued = false;

const msg = (e: unknown) => (e instanceof Error ? e.message : String(e));

interface State {
  /** False in the browser preview: there is no daemon, so there is no project data at all. */
  available: boolean;
  loaded: boolean;
  loading: boolean;
  error: string | null;
  projects: Project[];
  worktrees: Record<string, Worktree[]>;
  /** Per-project collapse state; projects are expanded unless explicitly collapsed. */
  expanded: Record<string, boolean>;
  /** Project shown in the Projects view. */
  selectedId: string | null;
  select: (id: string | null) => void;
  load: () => Promise<void>;
  add: (path: string) => Promise<Project>;
  pickAndAdd: () => Promise<Project | null>;
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
  expanded: loadExpanded(),
  selectedId: null,
  select: (selectedId) => set({ selectedId }),

  async load() {
    if (!inDesktop) { set({ loaded: true }); return; }
    if (get().loading) { reloadQueued = true; return; }
    set({ loading: true });
    try {
      const projects = await Projects.list();
      const lists = await Promise.all(projects.filter((p) => !p.missing).map(async (p) => [p.id, await Worktrees.list(p.id)] as const));
      set({ projects, worktrees: Object.fromEntries(lists), loaded: true, loading: false, error: null });
    } catch (e) {
      set({ loaded: true, loading: false, error: msg(e) });
    }
    if (reloadQueued) { reloadQueued = false; await get().load(); }
  },

  async add(path) {
    const p = await Projects.add(path);
    await get().load();
    return p;
  },

  async pickAndAdd() {
    const path = await Projects.pickFolder();
    return path ? get().add(path) : null;
  },

  async remove(id) {
    await Projects.remove(id);
    set((s) => ({ projects: s.projects.filter((p) => p.id !== id), worktrees: Object.fromEntries(Object.entries(s.worktrees).filter(([k]) => k !== id)), selectedId: s.selectedId === id ? null : s.selectedId }));
  },

  async open(id) { await Projects.open(id); },

  async setWorktreeRoot(id, root) {
    const p = await Projects.setWorktreeRoot(id, root);
    set((s) => ({ projects: s.projects.map((x) => (x.id === id ? p : x)) }));
  },

  async createWorktree(projectId, name, baseRef) {
    const wt = await Worktrees.create(projectId, name, baseRef);
    await get().load();
    return wt;
  },

  async removeWorktree(id, force) {
    await Worktrees.remove(id, force);
    await get().load();
  },

  toggle(id) {
    const next = { ...get().expanded, [id]: get().expanded[id] === false };
    localStorage.setItem(EXPANDED_KEY, JSON.stringify(next));
    set({ expanded: next });
  },

  startSync() {
    if (!inDesktop) return () => {};
    void get().load();
    const refresh = () => void get().load();
    const offs = [events.projectsChanged.on(refresh), events.worktreesChanged.on(refresh)];
    window.addEventListener("focus", refresh);
    return () => { offs.forEach((o) => o()); window.removeEventListener("focus", refresh); };
  },
}));

/** Branch state color key for a worktree: error, dirty, ahead/behind or clean. */
export function worktreeState(w: Worktree): "error" | "dirty" | "ahead" | "behind" | "clean" {
  if (w.error) return "error";
  if (w.dirty > 0) return "dirty";
  if (w.ahead > 0 && w.behind > 0) return "behind";
  if (w.ahead > 0) return "ahead";
  if (w.behind > 0) return "behind";
  return "clean";
}
