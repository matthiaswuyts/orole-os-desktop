import { lazy, useSyncExternalStore, type ComponentType } from "react";

export interface WidgetProps {
  widgetType: string;
  config?: Record<string, unknown>;
  onConfig: (patch: Record<string, unknown>) => void;
  onRemove: () => void;
}

export interface WidgetDef {
  type: string;
  title: string;
  description: string;
  w: number;
  h: number;
  component: ComponentType<WidgetProps>;
}

/** Widget bodies load on demand so charts and the grid's data code stay out of the entry chunk. */
const builtins: WidgetDef[] = [
  { type: "daemon-health", title: "Daemon", description: "Local daemon status, version, uptime and memory.", w: 4, h: 5, component: lazy(() => import("./builtin").then((m) => ({ default: m.DaemonHealthWidget }))) },
  { type: "clock", title: "Clock & calendar", description: "Current time and month calendar.", w: 4, h: 5, component: lazy(() => import("./builtin").then((m) => ({ default: m.ClockWidget }))) },
  { type: "notes", title: "Notes", description: "Free-form notes saved on the dashboard.", w: 6, h: 6, component: lazy(() => import("./builtin").then((m) => ({ default: m.NotesWidget }))) },
  { type: "setup", title: "Get started", description: "Remaining first-run setup steps.", w: 4, h: 5, component: lazy(() => import("./builtin").then((m) => ({ default: m.SetupWidget }))) },
  { type: "activity", title: "System activity", description: "Host CPU, memory, disk and uptime, plus daemon heap history.", w: 6, h: 9, component: lazy(() => import("@/features/system/HostActivity").then((m) => ({ default: m.HostActivity }))) },
  { type: "automation-results", title: "Automation results", description: "Recent daemon-owned automation runs and their status.", w: 6, h: 6, component: lazy(() => import("@/features/automations/AutomationResultsWidget").then((m) => ({ default: m.AutomationResultsWidget }))) },
  { type: "activity-heatmap", title: "Activity heat graph", description: "Agent tasks you started on each day of the last year.", w: 8, h: 6, component: lazy(() => import("@/features/home/ActivityHeatmapWidget").then((m) => ({ default: m.ActivityHeatmapWidget }))) },
];

const registry = new Map(builtins.map((widget) => [widget.type, widget]));
const listeners = new Set<() => void>();
let snapshot: readonly WidgetDef[] = builtins;
const getSnapshot = () => snapshot;
const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
};
const emit = () => {
  snapshot = [...registry.values()];
  listeners.forEach((listener) => listener());
};

/** Contributions share the built-in registry; cleanup only removes its own entries. */
export function registerWidgets(contributions: WidgetDef[]): () => void {
  const types = new Set<string>();
  for (const widget of contributions) {
    if (registry.has(widget.type) || types.has(widget.type)) throw new Error(`Widget type already registered: ${widget.type}`);
    types.add(widget.type);
  }
  for (const widget of contributions) registry.set(widget.type, widget);
  if (contributions.length) emit();
  return () => {
    let changed = false;
    for (const widget of contributions) {
      if (registry.get(widget.type) === widget) {
        registry.delete(widget.type);
        changed = true;
      }
    }
    if (changed) emit();
  };
}

export function useWidgetRegistry() {
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}
