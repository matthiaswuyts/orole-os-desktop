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
  { id: "w-term", type: "terminals", x: 0, y: 5, w: 6, h: 6 },
  { id: "w-notes", type: "notes", x: 6, y: 5, w: 6, h: 6 },
];

interface State {
  loaded: boolean;
  list: Ws[];
  activeId: string | null;
  error: string | null;
  load: () => Promise<void>;
  select: (id: string) => void;
  create: (name: string) => Promise<void>;
  remove: (id: string) => Promise<void>;
  setWidgets: (w: WidgetInstance[]) => void;
  addWidget: (type: string, w: number, h: number) => void;
  removeWidget: (id: string) => void;
  configureWidget: (id: string, config: Record<string, unknown>) => void;
}

async function listWs(): Promise<Workspace[]> {
  if (inDesktop) return Workspaces.list();
  try { return JSON.parse(localStorage.getItem(previewKey + ".ws") ?? "[]") as Workspace[]; } catch { return []; }
}
async function saveWs(w: Workspace): Promise<Workspace> {
  if (inDesktop) return Workspaces.save(w);
  const all = await listWs();
  const id = w.id || crypto.randomUUID();
  const next = { ...w, id };
  localStorage.setItem(previewKey + ".ws", JSON.stringify([...all.filter((x) => x.id !== id), next]));
  return next;
}
async function delWs(id: string) {
  if (inDesktop) return Workspaces.delete(id);
  localStorage.setItem(previewKey + ".ws", JSON.stringify((await listWs()).filter((x) => x.id !== id)));
}

const toWs = (w: Workspace): Ws => {
  const l = (w.layout ?? {}) as Partial<Layout>;
  return { id: w.id, name: w.name, position: w.position, layout: { widgets: Array.isArray(l.widgets) ? l.widgets : [] } };
};

let saveTimer: ReturnType<typeof setTimeout> | undefined;
function scheduleSave(get: () => State) {
  clearTimeout(saveTimer);
  saveTimer = setTimeout(() => {
    const ws = get().list.find((w) => w.id === get().activeId);
    if (ws) void saveWs({ id: ws.id, name: ws.name, position: ws.position, layout: ws.layout }).catch((e) => useWorkspaces.setState({ error: String(e) }));
  }, 400);
}

export const useWorkspaces = create<State>((set, get) => {
  const patchActive = (f: (w: Ws) => Ws) => {
    set((s) => ({ list: s.list.map((w) => (w.id === s.activeId ? f(w) : w)) }));
    scheduleSave(get);
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
    async remove(id) {
      if (get().list.length <= 1) return;
      await delWs(id);
      set((s) => {
        const list = s.list.filter((w) => w.id !== id);
        return { list, activeId: s.activeId === id ? list[0]!.id : s.activeId };
      });
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
