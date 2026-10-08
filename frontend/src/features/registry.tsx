import { lazy, type ComponentType } from "react";
import type { ViewId } from "@/stores/ui";
import { HomeView } from "@/features/home/HomeView";

/**
 * One entry per built view. A view missing here renders NotImplemented, so unbuilt modules never
 * show sample data. Each feature owns a features/<name>/ directory and adds exactly one line here.
 */
export const views: Partial<Record<ViewId, ComponentType>> = {
  home: HomeView,
  terminals: lazy(() => import("@/features/terminals/TerminalsView").then((m) => ({ default: m.TerminalsView }))),
  settings: lazy(() => import("@/features/settings/SettingsView").then((m) => ({ default: m.SettingsView }))),
};
