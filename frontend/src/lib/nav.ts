import { AppWindow, Bot, FolderGit2, FolderOpen, GitBranch, GitPullRequest, Globe, HardDrive, Home, Inbox, LayoutDashboard, Mail, Server, Settings, SquareTerminal, Workflow } from "lucide-react";
import type { ViewId } from "@/stores/ui";

export interface NavItem { id: ViewId; label: string; icon: typeof Home; title?: string; blurb?: string }

/** Primary rows, as in Shipyard's sidebar. */
export const PRIMARY: NavItem[] = [
  { id: "home", label: "Home", icon: Home },
  { id: "agents", label: "Agent Dashboard", icon: LayoutDashboard, blurb: "Every agent on every machine, by what it needs." },
  { id: "review", label: "Review", icon: Inbox, blurb: "Agents' finished work, to approve, send back or discard." },
  { id: "worktrees", label: "Worktrees", icon: GitBranch, blurb: "Every worktree of every project." },
  { id: "automations", label: "Automations", icon: Workflow, blurb: "Workflows that run on triggers." },
];
/** Behind "More". */
export const MORE: NavItem[] = [
  { id: "mail", label: "Mail", icon: Mail, blurb: "Orole Mail." },
  { id: "drive", label: "Drive", icon: HardDrive, blurb: "Orole Drive." },
  { id: "github", label: "GitHub", icon: GitPullRequest, blurb: "Repositories, issues and pull requests." },
  { id: "projects", label: "Projects", icon: FolderGit2, blurb: "Projects on your machines." },
  { id: "files", label: "Files", icon: FolderOpen, blurb: "Local and project files, with a conflict-aware editor." },
  { id: "terminals", label: "Terminals", icon: SquareTerminal, blurb: "Persistent terminals." },
  { id: "browser", label: "Browser preview", icon: Globe, blurb: "Sandboxed web previews at mobile, tablet and desktop widths." },
  { id: "machines", label: "Machines", icon: Server, blurb: "Local and remote machines." },
  { id: "applications", label: "Applications", icon: AppWindow, blurb: "Launcher for apps and shortcuts." },
];
export const SETTINGS: NavItem = { id: "settings", label: "Settings", icon: Settings };
export const NAV: NavItem[] = [...PRIMARY, ...MORE, SETTINGS];
export const navItem = (id: ViewId) => NAV.find((n) => n.id === id)!;
export { Bot };
