import type { HarnessOption, ModelOption } from "@/mygo";
import type { PermissionMode } from "@/features/tasks/permissionModes";

/**
 * How much a bot may do through integrations and tools without asking (chat.ToolAccess*). The levels are cumulative;
 * the daemon decides and audits every call, this only names the choice.
 */
export type ToolAccess = "ask" | "read" | "write" | "all";

export const TOOL_ACCESS: readonly { value: ToolAccess; label: string; hint: string }[] = [
  { value: "ask", label: "Ask me first", hint: "Every action through an integration or tool waits for your approval, unless you allowed it always." },
  { value: "read", label: "Look things up without asking", hint: "Reading and searching run at once. Anything that changes something still asks." },
  { value: "write", label: "Read and change without asking", hint: "Reading and changes such as sending mail or editing files run at once. Deleting and other destructive actions still ask." },
  { value: "all", label: "Everything without asking", hint: "Nothing asks, destructive actions included: a bot can delete mail, files or records on its own. Only for bots you fully trust." },
];

/** A stored level, or "" when unset or unknown (the daemon treats unknown values as "ask"). */
export function toolAccessOf(value: unknown): ToolAccess | "" {
  return value === "ask" || value === "read" || value === "write" || value === "all" ? value : "";
}

export const toolAccessLabel = (level: ToolAccess) => TOOL_ACCESS.find((entry) => entry.value === level)!.label;

/** Harness, model and effort may be left to the bot: it then picks one per task. */
export const TASK_AUTO = "auto";

/** The harnesses a bot can start: every installed one except custom commands (see daemon botTasks.Harnesses). */
export function botHarnesses(harnesses: readonly HarnessOption[] | undefined): HarnessOption[] {
  return (harnesses ?? []).filter((harness) => harness.id !== "custom");
}

export const harnessName = (harnesses: readonly HarnessOption[], id: string) => id === TASK_AUTO ? "Auto" : harnesses.find((harness) => harness.id === id)?.name || id;

export function modelName(harnesses: readonly HarnessOption[], harness: string, id: string): string {
  if (id === TASK_AUTO) return "Auto";
  return harnesses.find((h) => h.id === harness)?.models?.find((model) => model.id === id)?.name || id;
}

/** What a fixed harness and model allow: its models, and the effort levels of the model (or of the harness while the model is Auto). */
export function taskChoices(harnesses: readonly HarnessOption[], harness: string, model: string): { models: ModelOption[]; levels: string[] } {
  const option = harness === TASK_AUTO ? undefined : harnesses.find((h) => h.id === harness);
  if (!option) return { models: [], levels: [] };
  const models = option.models ?? [];
  const chosen = model === TASK_AUTO ? undefined : models.find((m) => m.id === model);
  return { models, levels: chosen ? chosen.reasoning ?? [] : option.reasoning ?? [] };
}

/**
 * The model and effort kept after the harness or model changes: a choice the new harness or model does not offer falls back
 * to Auto, so a bot is never told to use a model its harness lacks.
 */
export function fitTaskChoice(harnesses: readonly HarnessOption[], next: { harness: string; model: string; reasoning: string }): { model: string; reasoning: string } {
  const { models } = taskChoices(harnesses, next.harness, TASK_AUTO);
  const model = next.model === TASK_AUTO || models.some((m) => m.id === next.model) ? next.model : TASK_AUTO;
  const { levels } = taskChoices(harnesses, next.harness, model);
  const reasoning = next.reasoning === TASK_AUTO || levels.includes(next.reasoning) ? next.reasoning : TASK_AUTO;
  return { model, reasoning };
}

export const PERMISSION_LABELS: Record<PermissionMode, string> = { manual: "Manual", auto: "Auto", yolo: "Yolo" };

/** A stored task permission mode, or "" when unset or unknown. */
export function permissionModeOf(value: unknown): PermissionMode | "" {
  return value === "manual" || value === "auto" || value === "yolo" ? value : "";
}

/** Why a bot cannot start tasks in Yolo, or "" when it can. The daemon enforces the same two gates. */
export function botYoloBlocked(setting: { yoloDisabled?: boolean; botYolo?: boolean } | null | undefined): string {
  if (setting?.yoloDisabled) return "Yolo mode is turned off in Settings › System.";
  if (!setting?.botYolo) return "Turn on “Bots may use Yolo” in Settings › System first.";
  return "";
}
