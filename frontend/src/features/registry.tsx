import { lazy, type ComponentType } from "react";
import type { ViewId } from "@/stores/ui";
import "@/features/github/palette";
import "@/features/files/palette";
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
  terminals: lazy(() => import("@/features/terminals/TerminalsView").then((m) => ({ default: m.TerminalsView }))),
  projects: lazy(() => import("@/features/projects/ProjectsView").then((m) => ({ default: m.ProjectsView }))),
  files: lazy(() => import("@/features/files/FilesView").then((m) => ({ default: m.FilesView }))),
  worktrees: lazy(() => import("@/features/worktrees/WorktreesView").then((m) => ({ default: m.WorktreesView }))),
  github: lazy(() => import("@/features/github/GitHubView").then((m) => ({ default: m.GitHubView }))),
  mail: lazy(() => import("@/features/mail/MailView").then((m) => ({ default: m.MailView }))),
  drive: lazy(() => import("@/features/drive/DriveView").then((m) => ({ default: m.DriveView }))),
  browser: lazy(() => import("@/features/browser/BrowserView").then((m) => ({ default: m.BrowserView }))),
  automations: lazy(() => import("@/features/automations/AutomationsView").then((m) => ({ default: m.AutomationsView }))),
  machines: lazy(() => import("@/features/machines/MachinesView").then((m) => ({ default: m.MachinesView }))),
  applications: lazy(() => import("@/features/applications/ApplicationsView").then((m) => ({ default: m.ApplicationsView }))),
  settings: lazy(() => import("@/features/settings/SettingsView").then((m) => ({ default: m.SettingsView }))),
};
