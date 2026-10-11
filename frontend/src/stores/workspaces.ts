import { create } from "zustand";
import { Workspaces, type Workspace } from "@/mygo";
import { inDesktop, previewKey } from "@/lib/backend";

export interface WidgetInstance {
  id: string;
  type: string;
  x: number;
  y: number;
  w: number;
  h: number;
  config?: Record<string, unknown>;
}

export interface Layout {
  widgets: WidgetInstance[];
}

export interface Ws {
  id: string;
  name: string;
  position: number;
  layout: Layout;
}

export const DEFAULT_WIDGETS: WidgetInstance[] = [
  { id: "w-daemon", type: "daemon-health", x: 0, y: 0, w: 4, h: 5 },
  { id: "w-clock", type: "clock", x: 4, y: 0, w: 4, h: 5 },
  { id: "w-setup", type: "setup", x: 8, y: 0, w: 4, h: 5 },
  { id: "w-notes", type: "notes", x: 0, y: 5, w: 6, h: 6 },
  { id: "w-heat", type: "activity-heatmap", x: 0, y: 11, w: 12, h: 6 },
];

interface State {
  loaded: boolean;
  list: Ws[];
  activeId: string | null;
  error: string | null;
  load: () => Promise<void>;
  select: (id: string) => void;
  create: (name: string) => Promise<void>;
  rename: (id: string, name: string) => Promise<void>;
  remove: (id: string) => Promise<void>;
  setWidgets: (w: WidgetInstance[]) => void;
  addWidget: (type: string, w: number, h: number) => void;
  removeWidget: (id: string) => void;
  configureWidget: (id: string, config: Record<string, unknown>) => void;
}

// Serialize mutations so a slow save cannot overwrite a newer one or recreate
// a deleted workspace. Also protects preview localStorage read/modify/write.
let mutations: Promise<unknown> = Promise.resolve();
function mutate<T>(operation: () => Promise<T>): Promise<T> {
  const next = mutations.then(operation);
  mutations = next.catch(() => {});
  return next;
}

async function listWs(): Promise<Workspace[]> {
  if (inDesktop) return Workspaces.list();
  try { return JSON.parse(localStorage.getItem(previewKey + ".ws") ?? "[]") as Workspace[]; } catch { return []; }
}
function saveWs(w: Workspace): Promise<Workspace> {
  return mutate(async () => {
    if (inDesktop) return Workspaces.save(w);
    const all = await listWs();
    const id = w.id || crypto.randomUUID();
    const next = { ...w, id };
    localStorage.setItem(previewKey + ".ws", JSON.stringify([...all.filter((x) => x.id !== id), next]));
    return next;
  });
}
function delWs(id: string) {
  return mutate(async () => {
    if (inDesktop) return Workspaces.delete(id);
    localStorage.setItem(previewKey + ".ws", JSON.stringify((await listWs()).filter((x) => x.id !== id)));
  });
}

const toWs = (w: Workspace): Ws => {
  const l = (w.layout ?? {}) as Partial<Layout>;
  return { id: w.id, name: w.name, position: w.position, layout: { widgets: Array.isArray(l.widgets) ? l.widgets : [] } };
};

const saveTimers = new Map<string, number>();
const removing = new Set<string>();
function scheduleSave(ws: Ws) {
  window.clearTimeout(saveTimers.get(ws.id));
  saveTimers.set(ws.id, window.setTimeout(() => {
    saveTimers.delete(ws.id);
    void saveWs({ id: ws.id, name: ws.name, position: ws.position, layout: ws.layout })
      .catch((error: unknown) => useWorkspaces.setState({ error: String(error) }));
  }, 400));
}

export const useWorkspaces = create<State>((set, get) => {
  const patchActive = (f: (w: Ws) => Ws) => {
    const activeId = get().activeId;
    if (!activeId || removing.has(activeId)) return;
    set((s) => ({ list: s.list.map((w) => (w.id === activeId ? f(w) : w)) }));
    const ws = get().list.find((w) => w.id === activeId);
    if (ws) scheduleSave(ws);
  };
  return {
    loaded: false,
    list: [],
    activeId: null,
    error: null,
    async load() {
      try {
        let list = (await listWs()).map(toWs);
        if (list.length === 0) {
          const first = await saveWs({ id: "", name: "Development", position: 0, layout: { widgets: DEFAULT_WIDGETS } });
          list = [toWs(first)];
        }
        const last = localStorage.getItem("orole.activeWorkspace");
        set({ loaded: true, list, activeId: list.find((w) => w.id === last)?.id ?? list[0]!.id, error: null });
      } catch (e) {
        set({ loaded: true, error: e instanceof Error ? e.message : String(e) });
      }
    },
    select(id) {
      localStorage.setItem("orole.activeWorkspace", id);
      set({ activeId: id });
    },
    async create(name) {
      const saved = await saveWs({ id: "", name, position: get().list.length, layout: { widgets: [] } });
      set((s) => ({ list: [...s.list, toWs(saved)], activeId: saved.id }));
      localStorage.setItem("orole.activeWorkspace", saved.id);
    },
    async rename(id, name) {
      const ws = get().list.find((item) => item.id === id);
      name = name.trim();
      if (!ws || removing.has(id) || !name) return;
      const pending = saveTimers.has(id);
      window.clearTimeout(saveTimers.get(id));
      saveTimers.delete(id);
      set((s) => ({ list: s.list.map((item) => item.id === id ? { ...item, name } : item) }));
      try {
        await saveWs({ ...ws, name });
      } catch (error) {
        set((s) => ({ list: s.list.map((item) => item.id === id && item.name === name ? { ...item, name: ws.name } : item) }));
        const current = get().list.find((item) => item.id === id);
        if (pending && current && !removing.has(id)) scheduleSave(current);
        throw error;
      }
    },
    async remove(id) {
      if (removing.has(id) || get().list.filter((ws) => !removing.has(ws.id)).length <= 1) return;
      removing.add(id);
      const pending = saveTimers.has(id);
      window.clearTimeout(saveTimers.get(id));
      saveTimers.delete(id);
      try {
        await delWs(id);
        set((s) => {
          const list = s.list.filter((w) => w.id !== id);
          return { list, activeId: s.activeId === id ? list[0]!.id : s.activeId };
        });
      } catch (error) {
        const ws = get().list.find((w) => w.id === id);
        if (pending && ws) scheduleSave(ws);
        throw error;
      } finally {
        removing.delete(id);
      }
    },
    setWidgets: (widgets) => patchActive((w) => ({ ...w, layout: { ...w.layout, widgets } })),
    addWidget(type, w, h) {
      patchActive((ws) => {
        const maxY = ws.layout.widgets.reduce((m, x) => Math.max(m, x.y + x.h), 0);
        return { ...ws, layout: { ...ws.layout, widgets: [...ws.layout.widgets, { id: `w-${crypto.randomUUID().slice(0, 8)}`, type, x: 0, y: maxY, w, h }] } };
      });
    },
    removeWidget: (id) => patchActive((ws) => ({ ...ws, layout: { ...ws.layout, widgets: ws.layout.widgets.filter((x) => x.id !== id) } })),
    configureWidget: (id, config) =>
      patchActive((ws) => ({ ...ws, layout: { ...ws.layout, widgets: ws.layout.widgets.map((x) => (x.id === id ? { ...x, config: { ...x.config, ...config } } : x)) } })),
  };
});
