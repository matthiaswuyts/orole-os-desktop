import { lazy, type ComponentType } from "react";
import type { ViewId } from "@/stores/ui";
import "@/features/palette";
import { HomeView } from "@/features/home/HomeView";

/**
 * One entry per built view. A view missing here renders NotImplemented, so unbuilt modules never
 * show sample data. Each feature owns a features/<name>/ directory and adds exactly one line here.
 */
export const views: Partial<Record<ViewId, ComponentType>> = {
  home: HomeView,
  agents: lazy(() => import("@/features/agents/AgentsView").then((m) => ({ default: m.AgentsView }))),
  review: lazy(() => import("@/features/review/ReviewView").then((m) => ({ default: m.ReviewView }))),
  issues: lazy(() => import("@/features/issues/IssuesView").then((m) => ({ default: m.IssuesView }))),
  "new-task": lazy(() => import("@/features/projects/NewTaskView").then((m) => ({ default: m.NewTaskView }))),
  automations: lazy(() => import("@/features/automations/AutomationsView").then((m) => ({ default: m.AutomationsView }))),
  settings: lazy(() => import("@/features/settings/SettingsView").then((m) => ({ default: m.SettingsView }))),
  chat: lazy(() => import("./chat/ChatView").then((m) => ({ default: m.ChatView }))),
};
