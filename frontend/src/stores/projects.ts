import { create } from "zustand";
import { Projects, Worktrees, events, type Project, type Worktree } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { useUI, type ViewId } from "@/stores/ui";

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
  /** Reads ahead/behind and change counts per worktree (runs `git status`; slow in big repos), one project at a time. */
  loadStatus: () => Promise<void>;
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
      const git = projects.filter((p) => !p.missing && p.isGit);
      // The project list is one cheap query: show it at once. Worktrees fill in per project below.
      set((s) => ({ projects, loaded: true, error: null, worktrees: Object.fromEntries(git.flatMap((p) => s.worktrees[p.id] ? [[p.id, s.worktrees[p.id]!] as const] : [])) }));
      await Promise.all(git.map(async (p) => {
        try {
          const fresh = await Worktrees.list(p.id);
          set((s) => {
            if (!s.projects.some((x) => x.id === p.id)) return s;
            const known = new Map((s.worktrees[p.id] ?? []).filter((w) => w.statusKnown).map((w) => [w.id, w]));
            const merged = fresh.map((w) => { const old = known.get(w.id); return old && !w.error ? { ...w, upstream: old.upstream, ahead: old.ahead, behind: old.behind, staged: old.staged, unstaged: old.unstaged, untracked: old.untracked, dirty: old.dirty, statusKnown: true } : w; });
            return { worktrees: { ...s.worktrees, [p.id]: merged } };
          });
        } catch {
          // One unreadable repository must not hide every other project; its previous worktrees stay.
        }
      }));
      set({ loading: false });
    } catch (e) {
      set({ loaded: true, loading: false, error: msg(e) });
    }
    if (reloadQueued) { reloadQueued = false; await get().load(); }
  },

  async loadStatus() {
    if (!inDesktop) return;
    await Promise.all(get().projects.filter((p) => !p.missing && p.isGit).map(async (p) => {
      try {
        const list = await Worktrees.status(p.id);
        set((s) => s.projects.some((x) => x.id === p.id) ? { worktrees: { ...s.worktrees, [p.id]: list } } : s);
      } catch (e) {
        set((s) => ({ worktrees: { ...s.worktrees, [p.id]: (s.worktrees[p.id] ?? []).map((w) => ({ ...w, error: msg(e) })) } }));
      }
    }));
  },

  async create(name, path) {
    const p = await Projects.create(name.trim(), path);
    await get().load();
    return p;
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
