import type { ComponentType } from "react";
import { ActivityWidget, ClockWidget, DaemonHealthWidget, NotesWidget, SetupWidget, TerminalsWidget } from "./builtin";

export interface WidgetProps {
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

/** Built-in widgets. Integration widgets register here as their integrations land. */
export const widgets: WidgetDef[] = [
  { type: "daemon-health", title: "Daemon", description: "Local daemon status, version, uptime and memory.", w: 4, h: 5, component: DaemonHealthWidget },
  { type: "terminals", title: "Terminals", description: "Running terminal sessions.", w: 6, h: 6, component: TerminalsWidget },
  { type: "clock", title: "Clock & calendar", description: "Current time and month calendar.", w: 4, h: 5, component: ClockWidget },
  { type: "notes", title: "Notes", description: "Free-form notes saved with the workspace.", w: 6, h: 6, component: NotesWidget },
  { type: "setup", title: "Get started", description: "Remaining first-run setup steps.", w: 4, h: 5, component: SetupWidget },
  { type: "activity", title: "System activity", description: "Daemon memory over time.", w: 4, h: 5, component: ActivityWidget },
];

export const widgetByType = (t: string) => widgets.find((w) => w.type === t);
