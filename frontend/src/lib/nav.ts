import { CircleDot, Home, Inbox, LayoutDashboard, MessagesSquare, Settings, SquarePen, Workflow } from "lucide-react";
import type { ViewId } from "@/stores/ui";

export interface NavItem { id: ViewId; label: string; icon: typeof Home; title?: string; blurb?: string }

/** Main application sidebar rows, in order. */
export const PRIMARY: NavItem[] = [
  { id: "home", label: "Home", icon: Home },
  { id: "agents", label: "Tasks", icon: LayoutDashboard, blurb: "Every task, by what it needs from you." },
  { id: "review", label: "Review", icon: Inbox, blurb: "Finished work, to approve, send back or discard." },
  { id: "issues", label: "Issues", icon: CircleDot, blurb: "Find GitHub issues and hand them to an agent." },
  { id: "automations", label: "Automations", icon: Workflow, blurb: "Workflows that run on triggers." },
];
export const SETTINGS: NavItem = { id: "settings", label: "Settings", icon: Settings };
/** Reached from a project's "New task" control, never listed in the sidebar's main navigation. */
export const NEW_TASK: NavItem = { id: "new-task", label: "New task", icon: SquarePen };
/** The Chat mode's single view; the Code nav list (PRIMARY) never shows it. */
export const CHAT: NavItem = { id: "chat", label: "Chat", icon: MessagesSquare };
export const NAV: NavItem[] = [...PRIMARY, CHAT, SETTINGS, NEW_TASK];
export const navItem = (id: ViewId) => NAV.find((n) => n.id === id)!;
