import { create } from "zustand";
import { readAll, write } from "@/stores/settings";

export interface WidgetInstance {
  id: string;
  type: string;
  x: number;
  y: number;
  w: number;
  h: number;
  config?: Record<string, unknown>;
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
  widgets: WidgetInstance[];
  error: string | null;
  load: () => Promise<void>;
  setWidgets: (w: WidgetInstance[]) => void;
  addWidget: (type: string, w: number, h: number) => void;
  removeWidget: (id: string) => void;
  configureWidget: (id: string, config: Record<string, unknown>) => void;
}

// The Home dashboard is one layout stored under the `dashboard` setting. Saves are debounced and
// serialized so a slow write can never overwrite a newer one.
let saveTimer: number | undefined;
let writes: Promise<unknown> = Promise.resolve();

function scheduleSave() {
  window.clearTimeout(saveTimer);
  saveTimer = window.setTimeout(() => {
    const widgets = useDashboard.getState().widgets;
    writes = writes
      .then(() => write("dashboard", { widgets }))
      .then(() => useDashboard.setState({ error: null }))
      .catch((error: unknown) => useDashboard.setState({ error: error instanceof Error ? error.message : String(error) }));
  }, 400);
}

export const useDashboard = create<State>((set, get) => {
  const update = (widgets: WidgetInstance[]) => {
    set({ widgets });
    scheduleSave();
  };
  return {
    loaded: false,
    widgets: [],
    error: null,
    async load() {
      try {
        const saved = (await readAll()).dashboard as { widgets?: unknown } | undefined;
        set({ loaded: true, error: null, widgets: Array.isArray(saved?.widgets) ? (saved.widgets as WidgetInstance[]) : DEFAULT_WIDGETS });
      } catch (e) {
        set({ loaded: true, widgets: DEFAULT_WIDGETS, error: e instanceof Error ? e.message : String(e) });
      }
    },
    setWidgets: update,
    addWidget(type, w, h) {
      const widgets = get().widgets;
      const maxY = widgets.reduce((m, x) => Math.max(m, x.y + x.h), 0);
      update([...widgets, { id: `w-${crypto.randomUUID().slice(0, 8)}`, type, x: 0, y: maxY, w, h }]);
    },
    removeWidget: (id) => update(get().widgets.filter((x) => x.id !== id)),
    configureWidget: (id, config) => update(get().widgets.map((x) => (x.id === id ? { ...x, config: { ...x.config, ...config } } : x))),
  };
});
