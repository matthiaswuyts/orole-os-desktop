import { describe, expect, it } from "vitest";
import type { Event as AgentEvent } from "@/mygo";
import { groupEvents, liveActivity, mergeEvent, mergeHistory, shareItems, toolInputJSON, toolPreview, type FeedItem } from "./taskEvents";

let seq = 0;
const at = (s: number) => new Date(Date.UTC(2026, 9, 8, 12, 0, s)).toISOString();
const ev = (kind: string, text = "", data: unknown = null, sec = seq): AgentEvent => ({ id: ++seq, sessionId: "s", at: at(sec), kind, text, data });
const only = <T extends FeedItem["type"]>(items: FeedItem[], type: T) => items.filter((i): i is Extract<FeedItem, { type: T }> => i.type === type);

describe("groupEvents", () => {
  it("merges a tool call and its result by ToolData.id, wherever the result lands", () => {
    const items = groupEvents([
      ev("tool", "Bash: ls", { id: "t1", name: "Bash", status: "running", input: { command: "ls" } }),
      ev("tool", "mcp: search", { id: "t2", name: "search", server: "docs", status: "running", input: { query: "x" } }),
      ev("tool_result", "", { id: "t2", name: "search", status: "ok", output: "found" }),
      ev("tool_result", "", { id: "t1", name: "Bash", status: "error", output: "boom" }),
    ]);
    const tools = only(items, "tool");
    expect(tools).toHaveLength(2);
    expect(tools[0]!.call).toMatchObject({ id: "t1", name: "Bash", status: "error", output: "boom", server: "" });
    expect(tools[1]!.call).toMatchObject({ id: "t2", server: "docs", status: "ok", output: "found" });
    expect(toolPreview(tools[0]!.call)).toBe("ls");
    expect(toolInputJSON(tools[1]!.call)).toContain('"query": "x"');
  });

  it("keeps a running call running while the session is live and marks it unknown once it is not", () => {
    const events = [ev("tool", "", { id: "t1", name: "Read", status: "running", input: { file_path: "a.go" } })];
    expect(only(groupEvents(events, { live: true }), "tool")[0]!.call.status).toBe("running");
    expect(only(groupEvents(events, { live: false }), "tool")[0]!.call.status).toBe("unknown");
    expect(toolPreview(only(groupEvents(events), "tool")[0]!.call)).toBe("a.go");
  });

  it("pairs legacy data-less tool events in order and tolerates missing data", () => {
    const items = groupEvents([
      ev("tool", "Bash: npm test", { tool: "Bash" }),
      ev("tool", "Read: a.go"),
      ev("tool_result", "ok output", { error: false }),
      ev("tool_result", "no such file", { error: true }),
      ev("tool_result", "orphan"),
    ]);
    const tools = only(items, "tool");
    expect(tools).toHaveLength(3);
    expect(tools[0]!.call).toMatchObject({ name: "Bash", status: "ok", output: "ok output", linked: false });
    expect(tools[1]!.call).toMatchObject({ name: "Read", status: "error", output: "no such file" });
    expect(tools[2]!.call).toMatchObject({ status: "ok", output: "orphan" });
    expect(toolInputJSON(tools[0]!.call)).toBe("");
  });

  it("settles unanswered legacy calls at the turn boundary", () => {
    const items = groupEvents([ev("tool", "Bash: ls"), ev("turn_done", "done", { ok: true })]);
    expect(only(items, "tool")[0]!.call.status).toBe("unknown");
  });

  it("keeps reasoning as its own item and replaces a repeated reasoning id", () => {
    const items = groupEvents([
      ev("reasoning", "first thought", { id: "r1" }),
      ev("message", "hello"),
      ev("reasoning", "first thought, revised", { id: "r1" }),
      ev("reasoning", "second", { id: "r2" }),
      ev("reasoning", "no data at all"),
    ]);
    const reasoning = only(items, "reasoning");
    expect(reasoning.map((r) => r.text)).toEqual(["first thought, revised", "second", "no data at all"]);
    expect(items[0]!.type).toBe("reasoning");
    expect(items[1]!.type).toBe("assistant");
  });

  it("turns image events into image items and degrades one without a path to a note", () => {
    const items = groupEvents([
      ev("image", "a red fox", { id: "i1", path: "C:\\scratch\\fox.png", mime: "image/png", prompt: "a red fox, studio light" }),
      ev("image", "lost"),
    ]);
    expect(only(items, "image")[0]!.image).toEqual({ id: "i1", path: "C:\\scratch\\fox.png", mime: "image/png", prompt: "a red fox, studio light" });
    expect(only(items, "note")[0]!.text).toBe("lost");
  });

  it("builds permission items from full and from data-less events", () => {
    const items = groupEvents([
      ev("permission", "Write: a.txt", { id: "p1", tool: "Write", summary: "a.txt", inputJSON: '{"file_path":"a.txt"}', input: { file_path: "a.txt" }, at: at(1) }),
      ev("permission", "Bash: rm -rf x"),
    ]);
    const [full, bare] = only(items, "permission");
    expect(full!.request).toMatchObject({ id: "p1", tool: "Write", inputJSON: '{"file_path":"a.txt"}' });
    expect(bare!.request).toMatchObject({ tool: "Bash: rm -rf x", inputJSON: "" });
  });

  it("emits turn separators with the duration since the prompt, only showing failures' result", () => {
    const items = groupEvents([
      ev("prompt", "do it", null, 0),
      ev("message", "done", null, 30),
      ev("turn_done", "done", { ok: true, result: "done" }, 72),
      ev("turn_done", "", { ok: false, result: "bad" }, 80),
    ]);
    const turns = only(items, "turn");
    expect(turns[0]).toMatchObject({ ok: true, durationMs: 72_000 });
    expect(turns[1]).toMatchObject({ ok: false, result: "bad", durationMs: null });
  });

  it("handles system notes, decisions, needs_input, errors and ignores other kinds", () => {
    const items = groupEvents([ev("system", "Resumed"), ev("decision", "allow Bash", { id: "p", allow: true }), ev("needs_input"), ev("error", "oops"), ev("activity", "x"), ev("init", "", { model: "m" })]);
    expect(items.map((i) => i.type)).toEqual(["note", "note", "note", "error"]);
  });

  it("tolerates a data field that arrives as a JSON string or a scalar", () => {
    const items = groupEvents([ev("tool", "", '{"id":"t9","name":"Edit","status":"ok"}'), ev("tool", "Bash: x", 42)]);
    expect(only(items, "tool").map((t) => t.call.name)).toEqual(["Edit", "Bash"]);
  });
});

describe("event merging", () => {
  it("dedupes by id, keeps order and drops non-feed kinds", () => {
    const a = ev("prompt", "a"), b = ev("message", "b"), c = ev("activity", "c");
    let list: AgentEvent[] = [];
    for (const e of [b, a, b, c]) list = mergeEvent(list, e);
    expect(list.map((e) => e.id)).toEqual([a.id, b.id]);
    expect(mergeHistory([a], [b, a, c]).map((e) => e.id)).toEqual([a.id, b.id]);
  });

  it("inserts a late event at its sorted place and returns the same list for a repeat", () => {
    const events = Array.from({ length: 6 }, (_, i) => ev("message", `m${i}`));
    const held = [events[0]!, events[1]!, events[3]!, events[5]!];
    expect(mergeEvent(held, events[4]!).map((e) => e.id)).toEqual([0, 1, 3, 4, 5].map((i) => events[i]!.id));
    expect(mergeEvent(held, events[0]!)).toBe(held);
    expect(mergeEvent(held, events[3]!)).toBe(held);
    expect(mergeEvent(held, events[2]!).map((e) => e.id)).toEqual([0, 1, 2, 3, 5].map((i) => events[i]!.id));
    expect(mergeEvent([], events[0]!)).toEqual([events[0]]);
  });

  it("lets a later event win a repeated id when merging history", () => {
    const old = ev("message", "old");
    const replaced = { ...old, text: "new" };
    expect(mergeHistory([old], [replaced]).map((e) => e.text)).toEqual(["new"]);
  });
});

describe("shareItems", () => {
  const items = (events: AgentEvent[]) => groupEvents(events);

  it("keeps the identity of every item an event did not change, and replaces only the one it did", () => {
    const events = [ev("prompt", "go"), ev("message", "one"), ev("tool", "", { id: "t1", name: "Read", status: "running", input: { path: "a" } })];
    const first = items(events);
    const finished = [...events, ev("tool_result", "", { id: "t1", name: "Read", status: "ok", output: "done" })];
    const next = shareItems(first, items(finished));
    expect(next).not.toBe(first);
    expect(next[0]).toBe(first[0]);
    expect(next[1]).toBe(first[1]);
    expect(next[2]).not.toBe(first[2]);
    expect(only(next, "tool")[0]!.call.status).toBe("ok");
  });

  it("returns the previous list when an event changes nothing visible", () => {
    const events = [ev("prompt", "go"), ev("message", "one")];
    const first = items(events);
    expect(shareItems(first, items([...events, ev("usage", "", { tokens: 3 })]))).toBe(first);
  });

  it("compares data parsed from a JSON string by value", () => {
    const events = [ev("tool", "", '{"id":"t9","name":"Edit","status":"ok","input":{"a":1}}')];
    const first = items(events);
    expect(shareItems(first, items([...events]))).toBe(first);
  });

  it("settles every call of a finished turn without a result, across many turns", () => {
    const events: AgentEvent[] = [];
    for (let i = 0; i < 50; i++) events.push(ev("prompt", `p${i}`), ev("tool", "", { id: `t${i}`, name: "Bash", status: "running" }), ev("turn_done", "ok"));
    events.push(ev("prompt", "last"), ev("tool", "", { id: "open", name: "Bash", status: "running" }));
    const statuses = only(groupEvents(events), "tool").map((t) => t.call.status);
    expect(statuses.slice(0, 50).every((s) => s === "unknown")).toBe(true);
    expect(statuses[50]).toBe("running");
    expect(only(groupEvents(events, { live: false }), "tool")[50]!.call.status).toBe("unknown");
  });
});

describe("liveActivity", () => {
  it("shows Thinking… only while a live session is working and says so", () => {
    expect(liveActivity({ live: true, status: "working", activity: "Thinking…" })).toEqual({ kind: "thinking", label: "Thinking…" });
    expect(liveActivity({ live: true, status: "working", activity: "Running tests" })).toEqual({ kind: "working", label: "Running tests" });
    expect(liveActivity({ live: true, status: "working", activity: "" })?.label).toBe("Working…");
    expect(liveActivity({ live: true, status: "idle", activity: "Thinking…" })).toBeNull();
    expect(liveActivity({ live: false, status: "working", activity: "Thinking…" })).toBeNull();
  });
});
