import { CircleDot, Home, Inbox, LayoutDashboard, MessagesSquare, Settings, SquarePen, Workflow } from "lucide-react";
import type { ViewId } from "@/stores/ui";

export interface NavItem { id: ViewId; label: string; icon: typeof Home; title?: string; blurb?: string }

/** Main application sidebar rows, in order. */
export const PRIMARY: NavItem[] = [
  { id: "home", label: "Home", icon: Home },
  { id: "agents", label: "Tasks", icon: LayoutDashboard, blurb: "Every task, by what it needs from you." },
  { id: "review", label: "Review", icon: Inbox, blurb: "Finished work, to approve, send back or discard." },
  { id: "issues", label: "Issues", icon: CircleDot, blurb: "Find GitHub issues and hand them to an agent." },
];
export const SETTINGS: NavItem = { id: "settings", label: "Settings", icon: Settings };
/** Reached from a project's "New task" control, never listed in the sidebar's main navigation. */
export const NEW_TASK: NavItem = { id: "new-task", label: "New task", icon: SquarePen };
/** Views outside PRIMARY: a bot's conversation is opened from the Agents section, Automations is listed after PRIMARY. */
export const CHAT: NavItem = { id: "chat", label: "Chat", icon: MessagesSquare };
export const AUTOMATIONS: NavItem = { id: "automations", label: "Automations", icon: Workflow, blurb: "Workflows that run on triggers." };
export const NAV: NavItem[] = [...PRIMARY, CHAT, AUTOMATIONS, SETTINGS, NEW_TASK];
export const navItem = (id: ViewId) => NAV.find((n) => n.id === id)!;
