import { create } from "zustand";
import { arrayMove } from "@dnd-kit/sortable";
import { readAll, write } from "@/stores/settings";
import { WIDGET_SIZES, type WidgetSize } from "@/components/widgets/frame";

/** One widget on Home. The array order is the reading and layout order. */
export interface WidgetInstance {
  id: string;
  type: string;
  size: WidgetSize;
  /** Spans two grid rows, so shorter widgets can sit beside it. */
  tall?: boolean;
  config?: Record<string, unknown>;
}

/** Rows fill the 12 columns: Needs you is tall beside two rows of usage cards, then two tall lists. */
export const DEFAULT_WIDGETS: WidgetInstance[] = [
  { id: "w-overview", type: "overview", size: "full" },
  { id: "w-needs-you", type: "needs-you", size: "s", tall: true },
  { id: "w-sessions", type: "usage-sessions", size: "s" },
  { id: "w-tokens", type: "usage-tokens", size: "s" },
  { id: "w-spend", type: "usage-spend", size: "s" },
  { id: "w-usage-totals", type: "usage-totals", size: "s" },
  { id: "w-by-agent", type: "usage-by-agent", size: "m", tall: true },
  { id: "w-latest", type: "latest-sessions", size: "m", tall: true },
  { id: "w-recent", type: "recent-tasks", size: "l" },
  { id: "w-actions", type: "quick-actions", size: "s" },
  { id: "w-running", type: "running-now", size: "m" },
  { id: "w-notes", type: "notes", size: "m" },
  { id: "w-setup", type: "setup", size: "s" },
  { id: "w-clock", type: "clock", size: "s" },
  { id: "w-daemon", type: "daemon-health", size: "s" },
];

/** Widget types that were folded into another one; saved instances move to the new type. */
const RENAMED: Record<string, string> = { "activity-heatmap": "overview" };

const SIZE_IDS: Record<string, true> = { s: true, m: true, l: true, full: true };
const num = (v: unknown) => (typeof v === "number" && Number.isFinite(v) ? v : 0);
/** A saved entry as the former or the current layout wrote it; every optional field is checked before use. */
type SavedWidget = { id: string; type: string; size?: unknown; tall?: unknown; config?: unknown; x?: unknown; y?: unknown; w?: unknown; h?: unknown };

/** Nearest size to a react-grid-layout width out of 12 columns; a tie goes to the larger size. */
function sizeForWidth(w: number): WidgetSize {
  let best = WIDGET_SIZES[0]!;
  for (const size of WIDGET_SIZES) if (Math.abs(size.span - w) <= Math.abs(best.span - w)) best = size;
  return best.id;
}

/**
 * Reads a saved layout. Current layouts are an ordered list of `{id,type,size,tall?,config?}`.
 * Layouts saved by the former free-form grid (`{x,y,w,h}`) are ordered by row then column, their
 * width maps to the nearest size and a height of 9+ rows (about 400px) becomes tall. Renamed types
 * are carried over (the heat graph is part of Overview now); entries without an id or type are
 * dropped. Unknown types are kept so a disabled plugin's widget returns when it is re-enabled.
 */
export function normalizeWidgets(raw: unknown): WidgetInstance[] {
  if (!Array.isArray(raw)) return DEFAULT_WIDGETS;
  const entries = raw.filter((w): w is SavedWidget => typeof w === "object" && w !== null && typeof (w as SavedWidget).id === "string" && typeof (w as SavedWidget).type === "string");
  const legacy = entries.some((w) => typeof w.size !== "string" && (typeof w.x === "number" || typeof w.w === "number"));
  const ordered = legacy ? entries.map((w, i) => ({ w, i })).sort((a, b) => num(a.w.y) - num(b.w.y) || num(a.w.x) - num(b.w.x) || a.i - b.i).map((e) => e.w) : entries;
  const seen = new Set<string>();
  const out: WidgetInstance[] = [];
  for (const w of ordered) {
    if (seen.has(w.id)) continue;
    seen.add(w.id);
    const type = Object.hasOwn(RENAMED, w.type) ? RENAMED[w.type]! : w.type;
    const size: WidgetSize = type !== w.type ? "full" : typeof w.size === "string" && Object.hasOwn(SIZE_IDS, w.size) ? (w.size as WidgetSize) : sizeForWidth(num(w.w) || 6);
    const tall = legacy ? type === w.type && num(w.h) >= 9 : w.tall === true;
    const config = typeof w.config === "object" && w.config !== null && !Array.isArray(w.config) ? (w.config as Record<string, unknown>) : undefined;
    out.push({ id: w.id, type, size, ...(tall ? { tall } : {}), ...(config ? { config } : {}) });
  }
  return out;
}

interface State {
  loaded: boolean;
  widgets: WidgetInstance[];
  error: string | null;
  load: () => Promise<void>;
  setWidgets: (w: WidgetInstance[]) => void;
  addWidget: (type: string, size: WidgetSize, tall?: boolean) => void;
  removeWidget: (id: string) => void;
  /** Moves `id` to where `overId` is (drag and drop / keyboard reorder). */
  moveWidget: (id: string, overId: string) => void;
  resizeWidget: (id: string, size: WidgetSize) => void;
  setTall: (id: string, tall: boolean) => void;
  configureWidget: (id: string, config: Record<string, unknown>) => void;
  resetLayout: () => void;
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
  const patch = (id: string, fn: (w: WidgetInstance) => WidgetInstance) => update(get().widgets.map((w) => (w.id === id ? fn(w) : w)));
  return {
    loaded: false,
    widgets: [],
    error: null,
    async load() {
      try {
        const saved = (await readAll()).dashboard as { widgets?: unknown } | undefined;
        set({ loaded: true, error: null, widgets: normalizeWidgets(saved?.widgets) });
      } catch (e) {
        set({ loaded: true, widgets: DEFAULT_WIDGETS, error: e instanceof Error ? e.message : String(e) });
      }
    },
    setWidgets: update,
    addWidget: (type, size, tall) => update([...get().widgets, { id: `w-${crypto.randomUUID().slice(0, 8)}`, type, size, ...(tall ? { tall } : {}) }]),
    removeWidget: (id) => update(get().widgets.filter((x) => x.id !== id)),
    moveWidget(id, overId) {
      const widgets = get().widgets;
      const from = widgets.findIndex((w) => w.id === id), to = widgets.findIndex((w) => w.id === overId);
      if (from < 0 || to < 0 || from === to) return;
      update(arrayMove(widgets, from, to));
    },
    resizeWidget: (id, size) => patch(id, (w) => ({ ...w, size })),
    setTall: (id, tall) => patch(id, ({ tall: _tall, ...w }) => (tall ? { ...w, tall } : w)),
    configureWidget: (id, config) => patch(id, (w) => ({ ...w, config: { ...w.config, ...config } })),
    resetLayout: () => update(DEFAULT_WIDGETS),
  };
});
