import { Home, Inbox, LayoutDashboard, Plug, Settings, Sparkles, Workflow } from "lucide-react";
import type { ViewId } from "@/stores/ui";

export interface NavItem { id: ViewId; label: string; icon: typeof Home; title?: string; blurb?: string }

/** Sidebar rows, in order. Skills and MCP are configured once and serve every harness. */
export const PRIMARY: NavItem[] = [
  { id: "home", label: "Home", icon: Home },
  { id: "agents", label: "Tasks", icon: LayoutDashboard, blurb: "Every task, by what it needs from you." },
  { id: "review", label: "Review", icon: Inbox, blurb: "Finished work, to approve, send back or discard." },
  { id: "automations", label: "Automations", icon: Workflow, blurb: "Workflows that run on triggers." },
  { id: "skills", label: "Skills", icon: Sparkles, blurb: "Skills available to every harness." },
  { id: "mcp", label: "MCP", icon: Plug, blurb: "MCP servers available to every harness." },
];
export const SETTINGS: NavItem = { id: "settings", label: "Settings", icon: Settings };
export const NAV: NavItem[] = [...PRIMARY, SETTINGS];
export const navItem = (id: ViewId) => NAV.find((n) => n.id === id)!;
