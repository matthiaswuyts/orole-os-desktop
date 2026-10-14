import { useAgents } from "@/stores/agents";
import { useChat } from "@/stores/chat";
import { useUI } from "@/stores/ui";
import { openTask } from "@/features/projects/tasks";

/** What a notification is about, from the daemon's `target`: "task:<id>", "bot:<id>" or "automation:<id>". */
export function parseTarget(target: string): { type: "task" | "bot" | "automation"; id: string } | null {
  const at = target.indexOf(":");
  const type = target.slice(0, at);
  const id = target.slice(at + 1);
  if (at < 1 || !id || (type !== "task" && type !== "bot" && type !== "automation")) return null;
  return { type, id };
}

/** Opens what the notification is about. Returns false when it names nothing this window can open. */
export function openTarget(target: string): boolean {
  const t = parseTarget(target);
  if (!t) return false;
  if (t.type === "task") openTask(t.id);
  else if (t.type === "bot") useChat.getState().open(t.id);
  else useUI.getState().focusAutomation(t.id);
  return true;
}

/** True when the focused window already shows what the notification is about, so it needs no cue. */
export function targetOnScreen(target: string): boolean {
  const t = parseTarget(target);
  if (!t || document.hidden || !document.hasFocus()) return false;
  const { view } = useUI.getState();
  if (t.type === "task") return view === "agents" && useAgents.getState().openId === t.id;
  if (t.type === "bot") return view === "chat" && useChat.getState().selectedBotId === t.id;
  return false;
}
