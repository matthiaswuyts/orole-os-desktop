import { createElement } from "react";
import { NAV } from "@/lib/nav";
import { registerCommands, type Command } from "@/lib/commands";
import { useSettings } from "@/stores/settings";
import { useUI } from "@/stores/ui";
import { useProjects } from "@/stores/projects";
import { BUILTIN_THEMES } from "@/themes/builtin";

/** Built-in Orole OS commands. Called once at startup. */
export function registerCoreCommands() {
  const go: Command[] = NAV.map((n) => ({
    id: `nav.${n.id}`, title: `Go to ${n.label}`, category: "Navigate", keywords: [n.label, n.id],
    icon: createElement(n.icon), run: () => useUI.getState().setView(n.id),
  }));
  const s = () => useSettings.getState();
  const cmds: Command[] = [
    { id: "settings.skills", title: "Open Skills settings", category: "Settings", keywords: ["skills", "harness"], run: () => useUI.getState().openSettings("skills") },
    { id: "settings.mcp", title: "Open MCP settings", category: "Settings", keywords: ["mcp", "servers", "tools"], run: () => useUI.getState().openSettings("mcp") },
    ...BUILTIN_THEMES.map((theme): Command => ({
      id: `theme.${theme.id}`, title: `Theme: ${theme.label}`, category: "Appearance", keywords: [theme.id],
      run: () => s().set("appearance", { theme: theme.id }),
    })),
    { id: "density.toggle", title: "Toggle compact density", category: "Appearance", run: () => s().set("appearance", { density: s().appearance.density === "compact" ? "comfortable" : "compact" }) },
    { id: "ascii.pause", title: "Toggle background animation", category: "Appearance", keywords: ["ascii", "pause", "play"], run: () => s().set("ascii", { paused: !s().ascii.paused }) },
    { id: "ascii.hide", title: "Toggle ASCII background", category: "Appearance", run: () => s().set("ascii", { enabled: !s().ascii.enabled }) },
    { id: "task.new", title: "New task…", category: "Tasks", keywords: ["agent", "claude", "codex", "omp", "start"], run: () => useProjects.getState().openNewTask() },
  ];
  return registerCommands([...go, ...cmds]);
}
