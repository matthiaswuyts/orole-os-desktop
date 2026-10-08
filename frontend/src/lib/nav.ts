import { AppWindow, Bot, FolderGit2, GitPullRequest, HardDrive, Home, Mail, Server, Settings, SquareTerminal, Workflow } from "lucide-react";
import type { ViewId } from "@/stores/ui";

export const NAV: { id: ViewId; label: string; icon: typeof Home; group: "main" | "bottom" }[] = [
  { id: "home", label: "Home", icon: Home, group: "main" },
  { id: "mail", label: "Mail", icon: Mail, group: "main" },
  { id: "drive", label: "Drive", icon: HardDrive, group: "main" },
  { id: "github", label: "GitHub", icon: GitPullRequest, group: "main" },
  { id: "agents", label: "Agents", icon: Bot, group: "main" },
  { id: "projects", label: "Projects", icon: FolderGit2, group: "main" },
  { id: "terminals", label: "Terminals", icon: SquareTerminal, group: "main" },
  { id: "automations", label: "Automations", icon: Workflow, group: "main" },
  { id: "machines", label: "Machines", icon: Server, group: "main" },
  { id: "applications", label: "Applications", icon: AppWindow, group: "main" },
  { id: "settings", label: "Settings", icon: Settings, group: "bottom" },
];
