import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Issue, Label, Project } from "@/mygo";

const api = vi.hoisted(() => ({ issues: vi.fn() }));
vi.mock("@/mygo", () => ({ GitHub: api }));
import { loadIssues, matchesQuery, mergeLabels, rankLabels } from "./feed";
import { applyMention, activeTrigger, suggestMentions } from "./mentions";
import { issueScopes, repoFromRemote } from "./repos";
import { splitReferences } from "../github/references";

const label = (name: string, id = name.length): Label => ({ id, name, color: "ff0000", description: "" });
const issue = (over: Partial<Issue>): Issue => ({
  id: 1, number: 1, title: "Login loops", body: "", state: "open", state_reason: "", user: { login: "ana", id: 1, avatar_url: "", html_url: "", type: "User" },
  labels: [], assignees: [], milestone: null, comments: 0, html_url: "", repository_url: "", created_at: "2026-10-01T00:00:00Z", updated_at: "2026-10-01T00:00:00Z", closed_at: null, repo: "acme/shop", ...over,
});

describe("repoFromRemote", () => {
  it.each([
    ["https://github.com/Acme/Shop.git", "acme/shop"],
    ["git@github.com:acme/shop.git", "acme/shop"],
    ["ssh://git@github.com/acme/shop", "acme/shop"],
    ["https://gitlab.com/acme/shop", ""],
    ["https://github.com:8443/acme/shop", ""],
    ["https://github.com/acme/shop?x=1", ""],
    ["https://github.com/acme", ""],
    ["https://github.com/../shop", ""],
    ["C:\\code\\shop", ""],
  ])("%s -> %s", (remote, repo) => expect(repoFromRemote(remote)).toBe(repo));

  it("lists one scope per repository and skips projects without a GitHub remote", () => {
    const project = (id: string, remoteUrl: string) => ({ id, name: id, remoteUrl }) as Project;
    expect(issueScopes([project("a", "https://github.com/acme/shop"), project("b", "git@github.com:Acme/Shop.git"), project("c", ""), project("d", "https://github.com/acme/notes")]))
      .toEqual([{ id: "a", name: "a", repo: "acme/shop" }, { id: "d", name: "d", repo: "acme/notes" }]);
  });
});

describe("loadIssues", () => {
  beforeEach(() => { api.issues.mockReset(); });

  it("maps ownership to the API assignee filter and merges repositories newest first without pull requests", async () => {
    api.issues.mockImplementation(async ({ repo }: { repo: string }) => repo === "acme/shop"
      ? [issue({ number: 2, updated_at: "2026-10-02T00:00:00Z", repo }), issue({ number: 3, pull_request: { url: "x" }, updated_at: "2026-10-09T00:00:00Z", repo })]
      : [issue({ number: 7, updated_at: "2026-10-05T00:00:00Z", repo })]);
    const result = await loadIssues({ repos: ["acme/shop", "acme/notes"], state: "open", labels: ["bug"], ownership: "unassigned", me: "me", limit: 50 });
    expect(result.issues.map((i) => `${i.repo}#${i.number}`)).toEqual(["acme/notes#7", "acme/shop#2"]);
    expect(api.issues).toHaveBeenCalledWith(expect.objectContaining({ repo: "acme/shop", labels: ["bug"], assignee: "none", sort: "updated", limit: 50 }));
    await loadIssues({ repos: ["acme/shop"], state: "open", labels: [], ownership: "mine", me: "me", limit: 50 });
    expect(api.issues).toHaveBeenLastCalledWith(expect.objectContaining({ assignee: "me" }));
  });

  it("keeps the repositories that loaded when one fails and reports whether a page was full", async () => {
    api.issues.mockImplementation(async ({ repo }: { repo: string }) => {
      if (repo === "acme/gone") throw new Error("not_found: repository not found");
      return [issue({ number: 1, repo }), issue({ number: 2, repo })];
    });
    const result = await loadIssues({ repos: ["acme/shop", "acme/gone"], state: "all", labels: [], ownership: "all", me: "", limit: 2 });
    expect(result.issues).toHaveLength(2);
    expect(result.failures).toEqual([{ repo: "acme/gone", message: "repository not found" }]);
    expect(result.full).toBe(true);
  });

  it("never runs more than four repositories at once, yet loads all of them", async () => {
    let running = 0; let peak = 0;
    const release: (() => void)[] = [];
    api.issues.mockImplementation(() => {
      peak = Math.max(peak, ++running);
      const { promise, resolve } = Promise.withResolvers<Issue[]>();
      release.push(() => { running--; resolve([]); });
      return promise;
    });
    const done = loadIssues({ repos: Array.from({ length: 10 }, (_, i) => `acme/r${i}`), state: "open", labels: [], ownership: "all", me: "", limit: 10 });
    for (let index = 0; index < 10; index++) {
      await vi.waitFor(() => expect(release.length).toBeGreaterThan(index));
      release[index]!();
    }
    await done;
    expect(api.issues).toHaveBeenCalledTimes(10);
    expect(peak).toBe(4);
  });
});

describe("matchesQuery", () => {
  const target = issue({ number: 18204, title: "Login loops after reset", repo: "acme/shop", labels: [label("needs repro")], assignees: [{ login: "mira-k" } as Issue["user"]] });
  it.each([
    ["login reset", true], ["#18204", true], ["acme/shop#18204", true], ["needs repro", true], ["MIRA", true], ["ana", true],
    ["checkout", false], ["#1820", true], ["#999", false],
  ])("%s -> %s", (query, expected) => expect(matchesQuery(target, query)).toBe(expected));
});

describe("labels", () => {
  it("merges by case-insensitive name keeping the first and ranks selected, then most used", () => {
    const merged = mergeLabels([label("Bug", 1)], [label("bug", 2), label("docs", 3), label("infra", 4)]);
    expect(merged.map((l) => l.name)).toEqual(["Bug", "docs", "infra"]);
    const issues = [issue({ labels: [label("infra")] }), issue({ labels: [label("infra"), label("docs")] })];
    expect(rankLabels(merged, issues, ["docs"]).map((l) => l.name)).toEqual(["docs", "infra", "Bug"]);
  });
});

describe("mentions", () => {
  it("finds the trigger before the caret only at a word boundary", () => {
    expect(activeTrigger("hi @mi", 6)).toEqual({ sigil: "@", query: "mi", start: 3 });
    expect(activeTrigger("see #12", 7)).toEqual({ sigil: "#", query: "12", start: 4 });
    expect(activeTrigger("@", 1)).toEqual({ sigil: "@", query: "", start: 0 });
    expect(activeTrigger("hi @mira", 6)).toEqual({ sigil: "@", query: "mi", start: 3 });
    expect(activeTrigger("mail me@host", 12)).toBeNull();
    expect(activeTrigger("hi @mira ok", 11)).toBeNull();
  });

  it("suggests people by prefix before substring without duplicates, and issues by number before title", () => {
    const trigger = (sigil: "@" | "#", query: string) => ({ sigil, query, start: 0 });
    expect(suggestMentions(trigger("@", "ra"), ["tomas-r", "Mira", "mira", "rae"], []).map((o) => o.insert)).toEqual(["@rae", "@mira"]);
    const issues = [{ number: 4, title: "Fix 412 redirect" }, { number: 412, title: "Rotate credentials" }, { number: 99, title: "Docs for webhook" }];
    expect(suggestMentions(trigger("#", "41"), [], issues).map((o) => o.insert)).toEqual(["#412", "#4"]);
    expect(suggestMentions(trigger("#", "webhook"), [], issues).map((o) => o.insert)).toEqual(["#99"]);
    expect(suggestMentions(trigger("@", "zzz"), ["mira"], [])).toEqual([]);
  });

  it("replaces the typed trigger and keeps the text after the caret", () => {
    const text = "thanks @mi, please look";
    const trigger = activeTrigger(text, 10)!;
    const out = applyMention(text, 10, trigger, { key: "@mira-k", insert: "@mira-k", label: "@mira-k" });
    expect(out.text).toBe("thanks @mira-k , please look");
    expect(out.caret).toBe("thanks @mira-k ".length);
  });
});

describe("splitReferences", () => {
  it("separates mentions and issue references from text but leaves emails, URLs and team handles alone", () => {
    expect(splitReferences("cc @mira-k, see #412.")).toEqual([
      { kind: "text", value: "cc " }, { kind: "mention", value: "@mira-k" }, { kind: "text", value: ", see " }, { kind: "issue", value: "#412" }, { kind: "text", value: "." },
    ]);
    expect(splitReferences("me@host.com https://x.io/a#12 @org/team a#1")).toEqual([{ kind: "text", value: "me@host.com https://x.io/a#12 @org/team a#1" }]);
    expect(splitReferences("@a")).toEqual([{ kind: "mention", value: "@a" }]);
  });
});
