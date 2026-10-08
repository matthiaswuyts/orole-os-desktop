import { useSyncExternalStore, type ComponentType } from "react";
import { AssignedIssuesWidget, FailedCiWidget, GitHubNotificationsWidget, OpenPullsWidget } from "@/features/github/widgets";
import { AutomationResultsWidget } from "@/features/automations/AutomationResultsWidget";
import { MailWidget, DriveWidget } from "@/features/mcp/widgets";
import { ActivityWidget, ClockWidget, DaemonHealthWidget, NotesWidget, SetupWidget, TerminalsWidget } from "./builtin";

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

const builtins: WidgetDef[] = [
  { type: "daemon-health", title: "Daemon", description: "Local daemon status, version, uptime and memory.", w: 4, h: 5, component: DaemonHealthWidget },
  { type: "terminals", title: "Terminals", description: "Running terminal sessions.", w: 6, h: 6, component: TerminalsWidget },
  { type: "clock", title: "Clock & calendar", description: "Current time and month calendar.", w: 4, h: 5, component: ClockWidget },
  { type: "notes", title: "Notes", description: "Free-form notes saved with the workspace.", w: 6, h: 6, component: NotesWidget },
  { type: "setup", title: "Get started", description: "Remaining first-run setup steps.", w: 4, h: 5, component: SetupWidget },
  { type: "activity", title: "System activity", description: "Host CPU, memory, disk and uptime, plus daemon heap history.", w: 6, h: 9, component: ActivityWidget },
  { type: "github-issues", title: "Assigned issues", description: "Open GitHub issues assigned to you.", w: 6, h: 6, component: AssignedIssuesWidget },
  { type: "github-pulls", title: "Open pull requests", description: "Your open GitHub pull requests.", w: 6, h: 6, component: OpenPullsWidget },
  { type: "github-notifications", title: "GitHub notifications", description: "Unread GitHub notifications.", w: 6, h: 6, component: GitHubNotificationsWidget },
  { type: "github-ci", title: "Failed CI", description: "Recent failed workflow runs in your repositories.", w: 6, h: 6, component: FailedCiWidget },
  { type: "automation-results", title: "Automation results", description: "Recent daemon-owned automation runs and their status.", w: 6, h: 6, component: AutomationResultsWidget },
  { type: "mail", title: "Mail", description: "Messages from your connected MCP mailbox.", w: 6, h: 6, component: MailWidget },
  { type: "drive", title: "Drive files", description: "Files from your connected MCP Drive.", w: 6, h: 6, component: DriveWidget },
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
