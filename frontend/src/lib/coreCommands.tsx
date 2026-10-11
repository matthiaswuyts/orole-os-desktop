import { createElement } from "react";
import { Code } from "lucide-react";
import { CHAT, NAV } from "@/lib/nav";
import { registerCommands, type Command } from "@/lib/commands";
import { useSettings } from "@/stores/settings";
import { useUI } from "@/stores/ui";
import { useProjects } from "@/stores/projects";
import { BUILTIN_THEMES } from "@/themes/builtin";
import { useChat } from "@/stores/chat";
import { BotAvatar } from "@/components/shell/BotAvatar";
import type { Bot } from "@/mygo";

/** Built-in Orole OS commands. Called once at startup. */
export function registerCoreCommands() {
  const go: Command[] = NAV.filter((n) => n.id !== "chat").map((n) => ({
    id: `nav.${n.id}`, title: `Go to ${n.label}`, category: "Navigate", keywords: [n.label, n.id],
    icon: createElement(n.icon), run: () => useUI.getState().setView(n.id),
  }));
  const s = () => useSettings.getState();
  const cmds: Command[] = [
    { id: "mode.chat", title: "Switch to Chat", category: "Navigate", keywords: ["bots", "contacts", "conversation", "mode"], icon: createElement(CHAT.icon), run: () => useUI.getState().setMode("chat") },
    { id: "mode.code", title: "Switch to Code", category: "Navigate", keywords: ["tasks", "projects", "agents", "mode"], icon: createElement(Code), run: () => useUI.getState().setMode("code") },
    { id: "settings.skills", title: "Open Skills settings", category: "Settings", keywords: ["skills", "harness"], run: () => useUI.getState().openSettings("skills") },
    { id: "settings.mcp", title: "Open integrations and MCP servers", category: "Settings", keywords: ["integrations", "connectors", "mcp", "servers", "tools"], run: () => useUI.getState().openSettings("integrations") },
    ...BUILTIN_THEMES.map((theme): Command => ({
      id: `theme.${theme.id}`, title: `Theme: ${theme.label}`, category: "Appearance", keywords: [theme.id],
      run: () => s().set("appearance", { theme: theme.id }),
    })),
    { id: "density.toggle", title: "Toggle compact density", category: "Appearance", run: () => s().set("appearance", { density: s().appearance.density === "compact" ? "comfortable" : "compact" }) },
    { id: "ascii.pause", title: "Toggle background animation", category: "Appearance", keywords: ["ascii", "pause", "play"], run: () => s().set("ascii", { paused: !s().ascii.paused }) },
    { id: "ascii.hide", title: "Toggle ASCII background", category: "Appearance", run: () => s().set("ascii", { enabled: !s().ascii.enabled }) },
    { id: "task.new", title: "New task…", category: "Tasks", keywords: ["agent", "claude", "codex", "omp", "start"], run: () => useProjects.getState().openNewTask() },
  ];
  const offCommands = registerCommands([...go, ...cmds]);

  // Bots are contacts: every one is a palette entry. The chat store stays synced for the app's lifetime so they exist before Chat is opened.
  const stopChat = useChat.getState().startSync();
  let offBots = () => {};
  let shown = "";
  const syncBots = (bots: Bot[]) => {
    // Streaming previews replace the bots array many times a second; only identity fields matter here.
    const signature = bots.map((b) => `${b.id}\u0000${b.name}\u0000${b.avatar}\u0000${b.color}\u0000${b.description}`).join("\u0001");
    if (signature === shown) return;
    shown = signature;
    offBots();
    offBots = registerCommands(bots.map((bot): Command => ({
      id: `chat.bot.${bot.id}`, title: `Chat: ${bot.name}`, category: "Chat", keywords: [bot.name, bot.description, "bot", "chat"],
      icon: createElement(BotAvatar, { name: bot.name, avatar: bot.avatar }),
      run: () => useChat.getState().open(bot.id),
    })));
  };
  syncBots(useChat.getState().bots);
  const offStore = useChat.subscribe((state, previous) => { if (state.bots !== previous.bots) syncBots(state.bots); });
  return () => { offStore(); offBots(); stopChat(); offCommands(); };
}
