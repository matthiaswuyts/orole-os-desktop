import type { HarnessOption, ModelOption } from "@/mygo";

function claudeModel(model: Pick<ModelOption, "id" | "name">) {
  const name = model.name.trim();
  const resolved = name.match(/\((claude-[^)]+)\)/i)?.[1];
  const match = (resolved || name || model.id).match(/(?:claude[- /])?(opus|sonnet|haiku|fable)[- ](\d+)(?:[.-](\d+))?/i);
  if (!match) return null;
  const family = match[1]!.charAt(0).toUpperCase() + match[1]!.slice(1).toLowerCase();
  // A long trailing number is a release date (claude-haiku-4-5-20251001), not a minor version.
  const minor = match[3] && match[3].length < 4 ? match[3] : "0";
  const long = /\[1m\]|\(1m context\)/i.test(`${model.id} ${name}`);
  // Claude Code marks only its 1M variants; other windows are not reported, so none is claimed.
  return { name: `Claude ${family} ${match[2]}.${minor}`, context: long ? "1M" : "" };
}

/** Display-only formatting: selectors sent to the daemon are never rewritten. Claude models read "Claude Opus 5.0". */
export function modelLabel(model: Pick<ModelOption, "id" | "name"> | undefined): string {
  if (!model) return "Provider default";
  const claude = claudeModel(model);
  if (claude) return claude.name;
  const name = model.name.trim();
  const clean = (name || model.id).replace(/\s*\((?:default|recommended)\)\s*/gi, " ").replace(/^claude\s+/i, "").trim();
  if (!clean || /^default$/i.test(clean)) return "Provider default";
  return clean.replace(/^(?:openai\/)?gpt-/i, "GPT-");
}

/** The context window, like "1M", where the harness's catalog identifies it; empty when it does not. */
export function modelContext(model: Pick<ModelOption, "id" | "name"> | undefined): string {
  return model ? claudeModel(model)?.context ?? "" : "";
}

/** Name and context window together, for places with room for one label. */
export function modelTitle(model: Pick<ModelOption, "id" | "name"> | undefined): string {
  const context = modelContext(model);
  return context ? `${modelLabel(model)} · ${context} context` : modelLabel(model);
}

export function providerLabel(provider: Pick<HarnessOption, "id" | "name">): string {
  return provider.id === "omp" ? "Pi" : provider.name;
}

export function effortLabel(level: string): string {
  if (!level) return "Auto";
  if (level === "xhigh") return "X-High";
  return level.charAt(0).toUpperCase() + level.slice(1);
}
const effortOrder: Record<string, number> = { off: 0, none: 0, minimal: 1, low: 2, medium: 3, high: 4, xhigh: 5, max: 6 };


/** Auto means omit the override, not an invented provider effort level. */
export function effortChoices(levels: readonly string[]): string[] {
  return ["", ...new Set(levels.filter(Boolean))].sort((a, b) => (a === "" ? -1 : effortOrder[a] ?? 7) - (b === "" ? -1 : effortOrder[b] ?? 7));
}

export function supportedEffort(levels: readonly string[], current: string): string {
  return levels.includes(current) ? current : "";
}
