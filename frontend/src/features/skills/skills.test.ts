import { describe, expect, it } from "vitest";
import type { Discovered, Skill, SyncState } from "@/mygo";
import { draftProblem, filterSkills, gatewayBadge, groupDiscovered, isSlug, mergeSyncStates, needsSync, parseFrontmatter, retemplate, skillTemplate, slugify, sourceLabel } from "./skills";

const skill = (name: string, description = ""): Skill => ({ name, description, body: "", files: [], enabled: true, source: "orole", updatedAt: 0 });
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

describe("sourceLabel", () => {
  it.each([["orole", "Orole"], ["", "Orole"], ["imported:codex", "Imported from codex"], ["builtin", "Built in (bots)"]])("%j -> %j", (s, want) => expect(sourceLabel(s)).toBe(want));
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
  it("maps gateway states", () => {
    expect(gatewayBadge("installed").tone).toBe("success");
    expect(gatewayBadge("outdated").tone).toBe("warning");
    expect(gatewayBadge("missing").tone).toBe("error");
    expect(gatewayBadge("unsupported").tone).toBe("outline");
    expect(gatewayBadge("").label).toBe("Gateway unknown");
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
