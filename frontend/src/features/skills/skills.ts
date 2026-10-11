import type { Discovered, Skill, SyncState } from "@/mygo";

/** Lower-case slug used as the skill's directory name: letters, digits and single hyphens. */
export function slugify(input: string): string {
  return input.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "").slice(0, 64);
}

/** True when `name` is already a valid slug. The daemon validates again; this only guides the form. */
export const isSlug = (name: string) => /^[a-z0-9]+(-[a-z0-9]+)*$/.test(name) && name.length <= 64;

/** SKILL.md starting point for a new skill: frontmatter with name and description, then instructions. */
export function skillTemplate(name: string, description = ""): string {
  const slug = slugify(name);
  return `---\nname: ${slug}\ndescription: ${description.trim() || "Describe when an agent should use this skill."}\n---\n\n# ${slug || "skill"}\n\nWrite the instructions an agent should follow when this skill applies.\n`;
}

/**
 * Keeps an untouched template in step with the name field: an empty body or the template of the
 * previous name becomes the template of the next name. Any edited body is left alone.
 */
export function retemplate(body: string, prevName: string, nextName: string): string {
  if (!slugify(nextName)) return body;
  return body === "" || body === skillTemplate(prevName) ? skillTemplate(nextName) : body;
}

/** Reads the `name` and `description` keys of a SKILL.md frontmatter block. Missing block yields empty strings. */
export function parseFrontmatter(body: string): { name: string; description: string; hasFrontmatter: boolean } {
  const match = /^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/.exec(body.replace(/^\uFEFF/, ""));
  if (!match) return { name: "", description: "", hasFrontmatter: false };
  const out = { name: "", description: "", hasFrontmatter: true };
  for (const line of match[1]!.split(/\r?\n/)) {
    const kv = /^(name|description):\s*(.*)$/.exec(line);
    if (!kv) continue;
    out[kv[1] as "name" | "description"] = kv[2]!.trim().replace(/^(["'])(.*)\1$/, "$2");
  }
  return out;
}

/** Why a SKILL.md draft cannot be saved, or null when it can. */
export function draftProblem(name: string, body: string): string | null {
  if (!body.trim()) return "SKILL.md is empty.";
  const fm = parseFrontmatter(body);
  if (!fm.hasFrontmatter) return "SKILL.md must start with a --- frontmatter block holding name and description.";
  const effective = fm.name || name;
  if (!isSlug(effective)) return "The skill name must be lowercase letters, digits and hyphens.";
  if (!fm.description) return "The frontmatter needs a description so agents know when to use the skill.";
  return null;
}

/** Case-insensitive match on name and description. */
export function filterSkills(list: readonly Skill[], query: string): Skill[] {
  const q = query.trim().toLowerCase();
  const sorted = [...list].sort((a, b) => a.name.localeCompare(b.name));
  return q ? sorted.filter((s) => s.name.toLowerCase().includes(q) || s.description.toLowerCase().includes(q)) : sorted;
}

/** "orole" -> "Orole", "builtin" -> "Built in (bots)", "imported:claude" -> "Imported from claude". */
export function sourceLabel(source: string): string {
  if (source === "builtin") return "Built in (bots)";
  if (source.startsWith("imported:")) return `Imported from ${source.slice("imported:".length)}`;
  return source === "orole" || source === "" ? "Orole" : source;
}

/** Discovered skills grouped per harness, in harness order claude, codex, omp, then the rest alphabetically. */
export function groupDiscovered(list: readonly Discovered[]): { harness: string; skills: Discovered[] }[] {
  const order = ["claude", "codex", "omp"];
  const groups = new Map<string, Discovered[]>();
  for (const d of list) groups.set(d.harness, [...(groups.get(d.harness) ?? []), d]);
  const rank = (h: string) => { const i = order.indexOf(h); return i < 0 ? order.length : i; };
  return [...groups.entries()]
    .sort(([a], [b]) => rank(a) - rank(b) || a.localeCompare(b))
    .map(([harness, skills]) => ({ harness, skills: skills.sort((a, b) => a.name.localeCompare(b.name)) }));
}

export const HARNESS_LABEL: Record<string, string> = { claude: "Claude Code", codex: "Codex", omp: "Oh My Pi" };
export const harnessLabel = (harness: string) => HARNESS_LABEL[harness] ?? harness;

export type GatewayTone = "success" | "warning" | "error" | "outline";

/** Gateway entry state as badge text and tone. */
export function gatewayBadge(gateway: string): { label: string; tone: GatewayTone } {
  switch (gateway) {
    case "installed": return { label: "Gateway installed", tone: "success" };
    case "outdated": return { label: "Gateway outdated", tone: "warning" };
    case "missing": return { label: "Gateway missing", tone: "error" };
    case "unsupported": return { label: "Gateway not supported", tone: "outline" };
    default: return { label: `Gateway ${gateway || "unknown"}`, tone: "outline" };
  }
}

/** True when a Sync would change something for this harness. */
export function needsSync(state: SyncState): boolean {
  return state.available && ((state.outdated?.length ?? 0) > 0 || (state.missing?.length ?? 0) > 0 || state.gateway === "outdated" || state.gateway === "missing");
}

/** Replaces the entries of `current` by the matching harness entries of `next` (a Sync result may cover one harness). */
export function mergeSyncStates(current: readonly SyncState[], next: readonly SyncState[]): SyncState[] {
  const byHarness = new Map(next.map((s) => [s.harness, s]));
  const merged = current.map((s) => byHarness.get(s.harness) ?? s);
  for (const s of next) if (!current.some((c) => c.harness === s.harness)) merged.push(s);
  return merged;
}
