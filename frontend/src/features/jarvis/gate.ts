import { permissionModeOf, toolAccessOf } from "@/features/chat/botTrust";
import type { AgentsSetting } from "@/features/settings/AgentSafetySettings";
import type { FormState } from "@/features/chat/builder/botForm";

/**
 * Why Jarvis cannot be turned on for this bot, or "" when it can. A display-only mirror of the daemon's
 * jarvis.CheckGate (same conditions, same order, same words): phone calls have no approval prompt, so the bot needs
 * full permissions. Unset overrides fall back to the global settings exactly as the daemon resolves them; the daemon
 * checks again when the bot is saved.
 */
export function jarvisGate(form: Pick<FormState, "toolAccess" | "codeTasks" | "taskPermissionMode">, agents: AgentsSetting | null): string {
  const global = agents ?? {};
  const access = toolAccessOf(form.toolAccess) || toolAccessOf(global.botToolAccess) || "ask";
  const mode = permissionModeOf(form.taskPermissionMode) || permissionModeOf(global.botTasks?.permissionMode) || permissionModeOf(global.defaultPermissionMode) || "manual";
  if (access !== "all") return "Jarvis needs this bot's tool access set to All.";
  if (!form.codeTasks) return "Jarvis needs code tasks turned on for this bot.";
  if (mode !== "yolo") return "Jarvis needs this bot's code tasks set to YOLO.";
  if (global.yoloDisabled) return "Jarvis needs YOLO mode allowed in Task permissions.";
  if (!global.botYolo) return "Jarvis needs \"Bots may start YOLO tasks\" turned on in Bot code tasks.";
  return "";
}
