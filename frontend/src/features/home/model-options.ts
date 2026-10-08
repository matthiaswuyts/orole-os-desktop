import type { HarnessOption, ModelOption } from "@/mygo";

/** Display-only formatting: selectors sent to the daemon are never rewritten. */
export function modelLabel(model: Pick<ModelOption, "id" | "name"> | undefined): string {
  if (!model) return "Provider default";
  const name = model.name.trim();
  const resolved = name.match(/\((claude-[^)]+)\)/i)?.[1];
  const candidate = resolved || name || model.id;
  const claude = candidate.match(/(?:claude[- /])?(opus|sonnet|haiku|fable)[- ](\d+)(?:[.-](\d+))?/i);
  if (claude) {
    const family = claude[1]!.charAt(0).toUpperCase() + claude[1]!.slice(1).toLowerCase();
    const minor = claude[3] && claude[3].length < 4 ? `.${claude[3]}` : "";
    const context = /\[1m\]|\(1m context\)/i.test(`${model.id} ${name}`) ? " (1M)" : "";
    return `${family} ${claude[2]}${minor}${context}`;
  }
  const clean = candidate.replace(/\s*\((?:default|recommended)\)\s*/gi, " ").replace(/^claude\s+/i, "").trim();
  if (!clean || /^default$/i.test(clean)) return "Provider default";
  return clean.replace(/^(?:openai\/)?gpt-/i, "GPT-");
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
