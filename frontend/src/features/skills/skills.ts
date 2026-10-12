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

export type SourceKind = "orole" | "builtin" | "harness" | "hub" | "other";

/**
 * Where a skill came from, for the badge and the detail page. `creator` is who made it:
 * "You" for skills written in Orole, the owner for skills.sh, the agent's name for imports.
 */
export function sourceInfo(source: string): { kind: SourceKind; label: string; harness?: string; creator: string; detail?: string } {
  if (source === "builtin") return { kind: "builtin", label: "Built in", creator: "Orole" };
  if (source.startsWith("imported:")) {
    const harness = source.slice("imported:".length);
    return { kind: "harness", harness, label: `Imported from ${harnessLabel(harness)}`, creator: harnessLabel(harness) };
  }
  if (source.startsWith(HUB_PREFIX)) {
    const [owner = "", repo = ""] = source.slice(HUB_PREFIX.length).split("/");
    return { kind: "hub", label: "From skills.sh", creator: owner, detail: `${owner}/${repo}` };
  }
  if (source === "orole" || source === "") return { kind: "orole", label: "Created in Orole", creator: "You" };
  return { kind: "other", label: source, creator: source };
}

const UNITS: [number, string][] = [[60, "minute"], [3600, "hour"], [86400, "day"], [2592000, "month"], [31536000, "year"]];

/** "just now", "5 minutes ago", "1 day ago", "3 months ago". Unknown (0) times give "". */
export function timeAgo(unixSeconds: number, nowMs = Date.now()): string {
  if (!unixSeconds) return "";
  const seconds = Math.max(0, Math.floor(nowMs / 1000 - unixSeconds));
  if (seconds < 60) return "just now";
  const [size, unit] = [...UNITS].reverse().find(([s]) => seconds >= s)!;
  const n = Math.floor(seconds / size);
  return `${n} ${unit}${n === 1 ? "" : "s"} ago`;
}

/** Registry source of a skill added from skills.sh (mirrors skills.HubSourcePrefix in Go). */
export const HUB_PREFIX = "skills.sh:";
export const hubKey = (ref: { source: string; skillId: string }) => `${ref.source}/${ref.skillId}`;

/** Keys ("owner/repo/skill") of the skills.sh skills already in the registry. */
export function addedFromHub(list: readonly Skill[]): ReadonlySet<string> {
  return new Set(list.filter((s) => s.source.startsWith(HUB_PREFIX)).map((s) => s.source.slice(HUB_PREFIX.length)));
}

/** 2_000_000 -> "2M", 733_900 -> "733.9K", 950 -> "950". */
export function formatInstalls(n: number): string {
  const short = (value: number, unit: string) => `${value.toFixed(1).replace(/\.0$/, "")}${unit}`;
  if (n >= 1_000_000) return short(n / 1_000_000, "M");
  if (n >= 1_000) return short(n / 1_000, "K");
  return String(Math.max(0, Math.trunc(n)));
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

export type AgentTone = "success" | "warning" | "error" | "outline";

/** One agent (harness) in plain words: is it installed, and does it have the current skills? */
export function agentStatus(state: SyncState): { label: string; tone: AgentTone } {
  if (!state.available) return { label: "Not installed", tone: "outline" };
  if (state.error) return { label: "Problem", tone: "error" };
  if (needsSync(state)) return { label: "Needs update", tone: "warning" };
  return { label: "Up to date", tone: "success" };
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
