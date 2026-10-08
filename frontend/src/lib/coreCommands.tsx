import { createElement } from "react";
import { NAV } from "@/lib/nav";
import { registerCommands, type Command } from "@/lib/commands";
import { useSettings } from "@/stores/settings";
import { useUI } from "@/stores/ui";
import { useAgents } from "@/stores/agents";

/** Built-in Orole OS commands. Called once at startup. */
export function registerCoreCommands() {
  const go: Command[] = NAV.map((n) => ({
    id: `nav.${n.id}`, title: `Go to ${n.label}`, category: "Navigate", keywords: [n.label, n.id],
    icon: createElement(n.icon), run: () => useUI.getState().setView(n.id),
  }));
  const s = () => useSettings.getState();
  const cmds: Command[] = [
    { id: "theme.midnight", title: "Theme: Orole Midnight", category: "Appearance", run: () => s().set("appearance", { theme: "midnight" }) },
    { id: "theme.obsidian", title: "Theme: Orole Obsidian", category: "Appearance", run: () => s().set("appearance", { theme: "obsidian" }) },
    { id: "theme.light", title: "Theme: Orole Light", category: "Appearance", run: () => s().set("appearance", { theme: "light" }) },
    { id: "density.toggle", title: "Toggle compact density", category: "Appearance", run: () => s().set("appearance", { density: s().appearance.density === "compact" ? "comfortable" : "compact" }) },
    { id: "ascii.pause", title: "Toggle background animation", category: "Appearance", keywords: ["ascii", "pause", "play"], run: () => s().set("ascii", { paused: !s().ascii.paused }) },
    { id: "ascii.hide", title: "Toggle ASCII background", category: "Appearance", run: () => s().set("ascii", { enabled: !s().ascii.enabled }) },
    { id: "terminal.new", title: "New terminal", category: "Terminals", keywords: ["shell", "powershell"], run: () => useUI.getState().setView("terminals") },
    { id: "agent.new", title: "New agent…", category: "Agents", keywords: ["claude", "codex", "start"], run: () => { useUI.getState().setView("agents"); useAgents.getState().setStartDialogOpen(true); } },
  ];
  return registerCommands([...go, ...cmds]);
}
