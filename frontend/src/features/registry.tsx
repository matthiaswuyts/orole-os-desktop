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
  automations: lazy(() => import("@/features/automations/AutomationsView").then((m) => ({ default: m.AutomationsView }))),
  skills: lazy(() => import("@/features/skills/SkillsView").then((m) => ({ default: m.SkillsView }))),
  mcp: lazy(() => import("@/features/mcp/McpView").then((m) => ({ default: m.McpView }))),
  settings: lazy(() => import("@/features/settings/SettingsView").then((m) => ({ default: m.SettingsView }))),
};
