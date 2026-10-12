import { describe, expect, it } from "vitest";
import type { Discovered, Skill, SyncState } from "@/mygo";
import { addedFromHub, agentStatus, draftProblem, filterSkills, formatInstalls, groupDiscovered, isSlug, mergeSyncStates, needsSync, parseFrontmatter, retemplate, skillTemplate, slugify, sourceInfo, timeAgo } from "./skills";

const skill = (name: string, description = ""): Skill => ({ name, description, body: "", files: [], enabled: true, source: "orole", addedAt: 0, updatedAt: 0 });
const state = (over: Partial<SyncState>): SyncState => ({ harness: "claude", name: "Claude Code", available: true, dir: "", synced: [], outdated: [], missing: [], gateway: "installed", error: "", ...over });

describe("slugify / isSlug", () => {
  it.each([
    ["My Skill", "my-skill"],
    ["  --Hello__World!! ", "hello-world"],
    ["", ""],
    ["a".repeat(80), "a".repeat(64)],
  ])("slugify(%j) = %j", (input, want) => expect(slugify(input)).toBe(want));
  it.each([["ok-name", true], ["Bad", false], ["a--b", false], ["-a", false], ["", false], ["a".repeat(65), false]])("isSlug(%j) = %j", (n, want) => expect(isSlug(n)).toBe(want));
});

describe("skillTemplate", () => {
  it("builds frontmatter that parses back to the slug", () => {
    const body = skillTemplate("Code Review");
    expect(body.startsWith("---\nname: code-review\ndescription: ")).toBe(true);
    expect(parseFrontmatter(body)).toMatchObject({ name: "code-review", hasFrontmatter: true });
    expect(parseFrontmatter(body).description).not.toBe("");
  });
  it("uses a given description", () => expect(parseFrontmatter(skillTemplate("x", " Does x ")).description).toBe("Does x"));
});

describe("retemplate", () => {
  it("fills an empty body once the name has a slug", () => {
    expect(retemplate("", "", "")).toBe("");
    expect(retemplate("", "", "demo")).toBe(skillTemplate("demo"));
  });
  it("follows the name while the template is untouched", () => expect(retemplate(skillTemplate("de"), "de", "demo")).toBe(skillTemplate("demo")));
  it("leaves an edited body alone", () => {
    const edited = skillTemplate("de") + "more";
    expect(retemplate(edited, "de", "demo")).toBe(edited);
  });
});

describe("parseFrontmatter", () => {
  it("reads quoted values and CRLF", () => {
    expect(parseFrontmatter('---\r\nname: "a-b"\r\ndescription: \'Does it\'\r\n---\r\nBody')).toEqual({ name: "a-b", description: "Does it", hasFrontmatter: true });
  });
  it("reports a missing block", () => expect(parseFrontmatter("# just text")).toEqual({ name: "", description: "", hasFrontmatter: false }));
  it("ignores a BOM", () => expect(parseFrontmatter("\uFEFF---\nname: x\n---\n").name).toBe("x"));
});

describe("draftProblem", () => {
  it("accepts a valid draft", () => expect(draftProblem("demo", skillTemplate("demo"))).toBeNull());
  it.each([
    ["empty", "demo", "  "],
    ["no frontmatter", "demo", "text"],
    ["bad name", "Bad Name", "---\nname: Bad Name\ndescription: x\n---\n"],
    ["no description", "demo", "---\nname: demo\n---\n"],
  ])("rejects %s", (_n, name, body) => expect(draftProblem(name, body)).not.toBeNull());
  it("lets the frontmatter name win", () => expect(draftProblem("", "---\nname: ok\ndescription: d\n---\n")).toBeNull());
});

describe("filterSkills", () => {
  const list = [skill("zeta", "last"), skill("alpha", "Reviews Code"), skill("beta")];
  it("sorts by name", () => expect(filterSkills(list, "").map((s) => s.name)).toEqual(["alpha", "beta", "zeta"]));
  it("matches name and description case-insensitively", () => {
    expect(filterSkills(list, "REVIEWS").map((s) => s.name)).toEqual(["alpha"]);
    expect(filterSkills(list, "et").map((s) => s.name)).toEqual(["beta", "zeta"]);
  });
});

describe("sourceInfo", () => {
  it.each([
    ["orole", "orole", "Created in Orole", "You"],
    ["", "orole", "Created in Orole", "You"],
    ["imported:codex", "harness", "Imported from Codex", "Codex"],
    ["imported:claude", "harness", "Imported from Claude Code", "Claude Code"],
    ["builtin", "builtin", "Built in", "Orole"],
    ["skills.sh:acme/skills/seo", "hub", "From skills.sh", "acme"],
  ])("%j", (source, kind, label, creator) => expect(sourceInfo(source)).toMatchObject({ kind, label, creator }));
  it("keeps the repository of a skills.sh skill", () => expect(sourceInfo("skills.sh:acme/skills/seo").detail).toBe("acme/skills"));
});

describe("timeAgo", () => {
  const now = 1_700_000_000_000;
  const ago = (seconds: number) => timeAgo(now / 1000 - seconds, now);
  it.each([[5, "just now"], [60, "1 minute ago"], [150, "2 minutes ago"], [3600, "1 hour ago"], [86400 * 2, "2 days ago"], [86400 * 65, "2 months ago"], [86400 * 800, "2 years ago"]])("%i s -> %s", (s, want) => expect(ago(s)).toBe(want));
  it("is empty when the time is unknown", () => expect(timeAgo(0, now)).toBe(""));
});

describe("groupDiscovered", () => {
  const d = (harness: string, name: string): Discovered => ({ harness, name, description: "", path: "" });
  it("orders harnesses and skills", () => {
    const groups = groupDiscovered([d("omp", "b"), d("custom", "z"), d("claude", "y"), d("claude", "a")]);
    expect(groups.map((g) => g.harness)).toEqual(["claude", "omp", "custom"]);
    expect(groups[0]!.skills.map((s) => s.name)).toEqual(["a", "y"]);
  });
  it("is empty for nothing found", () => expect(groupDiscovered([])).toEqual([]));
});

describe("sync state helpers", () => {
  it("agentStatus says what to do in plain words", () => {
    expect(agentStatus(state({}))).toEqual({ label: "Up to date", tone: "success" });
    expect(agentStatus(state({ missing: ["a"] }))).toEqual({ label: "Needs update", tone: "warning" });
    expect(agentStatus(state({ gateway: "outdated" }))).toEqual({ label: "Needs update", tone: "warning" });
    expect(agentStatus(state({ error: "boom", missing: ["a"] })).tone).toBe("error");
    expect(agentStatus(state({ available: false, missing: ["a"] }))).toEqual({ label: "Not installed", tone: "outline" });
  });
  it("needsSync only for installed harnesses with drift", () => {
    expect(needsSync(state({}))).toBe(false);
    expect(needsSync(state({ missing: ["a"] }))).toBe(true);
    expect(needsSync(state({ outdated: ["a"] }))).toBe(true);
    expect(needsSync(state({ gateway: "missing" }))).toBe(true);
    expect(needsSync(state({ available: false, missing: ["a"], gateway: "missing" }))).toBe(false);
  });
  it("mergeSyncStates replaces by harness and appends new ones", () => {
    const cur = [state({ harness: "claude" }), state({ harness: "codex", name: "Codex" })];
    const next = [state({ harness: "codex", name: "Codex", synced: ["a"] }), state({ harness: "omp", name: "Oh My Pi" })];
    const merged = mergeSyncStates(cur, next);
    expect(merged.map((s) => s.harness)).toEqual(["claude", "codex", "omp"]);
    expect(merged[1]!.synced).toEqual(["a"]);
  });
});

describe("skills.sh helpers", () => {
  it.each([[0, "0"], [999, "999"], [1000, "1K"], [1550, "1.6K"], [733_900, "733.9K"], [2_000_000, "2M"], [3_807_588, "3.8M"], [-4, "0"]])("formatInstalls(%i) = %s", (n, want) => expect(formatInstalls(n)).toBe(want));
  it("addedFromHub keys only skills that came from skills.sh", () => {
    const list = [{ ...skill("a"), source: "skills.sh:acme/skills/seo" }, skill("b"), { ...skill("c"), source: "imported:claude" }];
    expect([...addedFromHub(list)]).toEqual(["acme/skills/seo"]);
  });
});
