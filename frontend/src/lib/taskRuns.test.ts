import { describe, expect, it } from "vitest";
import type { Event as AgentEvent } from "@/mygo";
import { groupEvents, groupRuns, reasoningTitle, type RunContext, type TurnEntry, type TurnRun } from "./taskEvents";

let seq = 0;
const at = (s: number) => new Date(Date.UTC(2026, 9, 8, 12, 0, s)).toISOString();
const ev = (kind: string, text = "", data: unknown = null, sec = 0): AgentEvent => ({ id: ++seq, sessionId: "s", at: at(sec), kind, text, data });
const ctx = (patch: Partial<RunContext> = {}): RunContext => ({ running: false, live: true, pendingPermissions: new Set(), pendingQuestions: new Set(), ...patch });
const run = (entries: TurnEntry[], index = 0): TurnRun => {
  const found = entries.filter((entry): entry is TurnRun => entry.type === "run")[index];
  if (!found) throw new Error("no run");
  return found;
};
const keys = (items: { type: string; key: string }[]) => items.map((item) => item.type);
const group = (events: AgentEvent[], c: RunContext = ctx()) => groupRuns(groupEvents(events, { live: c.live }), c);

describe("groupRuns", () => {
  it("splits turns at user messages and folds reasoning, tools and interim messages into the run in order", () => {
    const entries = group([
      ev("prompt", "fix it", null, 0),
      ev("reasoning", "**Planning the fix**\n\nLook at the code.", { id: "r1" }, 1),
      ev("tool", "", { id: "t1", name: "Read", status: "running", input: { file_path: "a.go" } }, 2),
      ev("tool_result", "", { id: "t1", status: "ok", output: "x" }, 3),
      ev("message", "I found it, applying the change.", null, 4),
      ev("tool", "", { id: "t2", name: "apply_patch", status: "running", input: {} }, 5),
      ev("tool_result", "", { id: "t2", status: "ok" }, 6),
      ev("message", "Done: fixed.", null, 7),
      ev("turn_done", "Done: fixed.", { ok: true }, 8),
      ev("prompt", "thanks", null, 20),
      ev("message", "You're welcome.", null, 21),
      ev("turn_done", "", { ok: true }, 22),
    ]);
    expect(entries.map((entry) => entry.type)).toEqual(["item", "run", "item", "item"]);
    const first = run(entries);
    expect(keys(first.steps)).toEqual(["reasoning", "tool", "assistant", "tool"]);
    expect(first.steps[2]).toMatchObject({ type: "assistant", text: "I found it, applying the change." });
    expect(first.answer).toMatchObject([{ type: "assistant", text: "Done: fixed." }]);
    expect(first.attention).toEqual([]);
    // A plain question-and-answer turn needs no disclosure: its message is an ordinary row.
    expect(entries[3]).toMatchObject({ type: "item", item: { type: "assistant", text: "You're welcome." } });
  });

  it("derives the duration from the turn's first and last event timestamps, and omits it when unknown", () => {
    const events = [ev("prompt", "go", null, 0), ev("tool", "", { id: "t", name: "Bash", status: "ok", input: { command: "ls" } }, 10), ev("message", "ok", null, 133), ev("turn_done", "", { ok: true }, 133)];
    expect(run(group(events)).durationMs).toBe(133_000);
    expect(run(group([ev("prompt", "go", null, 5), ev("tool", "", { id: "u", name: "Bash", status: "ok", input: {} }, 5), ev("turn_done", "", { ok: true }, 5)])).durationMs).toBeNull();
    expect(run(group([ev("tool", "", { id: "v", name: "Bash", status: "ok", input: {} }, 5), ev("turn_done", "", { ok: true }, 9)])).durationMs).toBe(4000);
    expect(run(group([ev("prompt", "go", null, 0), ev("tool", "", { id: "w", name: "Bash", status: "running", input: {} }, 40)], ctx({ running: true }))).durationMs).toBeNull();
  });

  it("maps the turn outcome to a run status", () => {
    const tool = (id: string) => ev("tool", "", { id, name: "Bash", status: "ok", input: {} }, 1);
    expect(run(group([ev("prompt", "a"), tool("a"), ev("turn_done", "", { ok: true }, 2)])).status).toBe("complete");
    expect(run(group([ev("prompt", "a"), tool("b"), ev("turn_done", "boom", { ok: false, result: "boom" }, 2)])).status).toBe("error");
    expect(run(group([ev("prompt", "a"), tool("c"), ev("turn_done", "", { ok: false, result: "Turn interrupted by the user" }, 2)])).status).toBe("cancelled");
    expect(run(group([ev("prompt", "a"), tool("d")], ctx({ running: true }))).status).toBe("running");
    expect(run(group([ev("prompt", "a"), tool("e")], ctx({ live: false }))).status).toBe("incomplete");
    // An earlier turn that never reported completion is not "running" just because a later turn is.
    const entries = group([ev("prompt", "a"), tool("f"), ev("prompt", "b"), tool("g")], ctx({ running: true }));
    expect([run(entries, 0).status, run(entries, 1).status]).toEqual(["complete", "running"]);
  });

  it("shows a prompt the agent has not answered yet as a running run without steps", () => {
    const entries = group([ev("prompt", "hello")], ctx({ running: true }));
    expect(entries.map((entry) => entry.type)).toEqual(["item", "run"]);
    expect(run(entries)).toMatchObject({ status: "running", steps: [], answer: [], attention: [] });
    expect(group([ev("prompt", "hello")]).map((entry) => entry.type)).toEqual(["item"]);
  });

  it("keeps pending approvals and questions in the attention slot, never inside the collapsed steps", () => {
    const events = [
      ev("prompt", "go"),
      ev("tool", "", { id: "t", name: "Bash", status: "running", input: { command: "ls" } }),
      ev("permission", "Bash", { id: "p1", tool: "Bash", summary: "rm x", input: { command: "rm x" }, inputJSON: '{"command":"rm x"}' }),
      ev("question", "", { id: "q1", questions: [{ id: "a", prompt: "Which?", options: [] }] }),
      ev("permission", "Bash", { id: "p0", tool: "Bash", summary: "ls", input: {}, inputJSON: "{}" }),
      ev("decision", "", { id: "p0", allow: true, reason: "" }),
    ];
    const entries = group(events, ctx({ pendingPermissions: new Set(["p1"]), pendingQuestions: new Set(["q1"]) }));
    const turn = run(entries);
    expect(turn.status).toBe("requires-action");
    expect(turn.attention.map((item) => item.type === "permission" ? item.request.id : item.type === "question" ? item.request.id : item.type)).toEqual(["p1", "q1"]);
    // A decided approval is history: it folds into the run with the rest.
    expect(turn.steps.map((item) => item.type)).toEqual(["tool", "permission"]);
    // Once the daemon no longer lists the request, nothing pending is left to hide.
    expect(run(group(events, ctx())).attention).toEqual([]);
  });

  it("keeps errors, notices and a failed turn's result visible and files and images with the answer", () => {
    const entries = group([
      ev("prompt", "go"),
      ev("tool", "", { id: "t", name: "Bash", status: "ok", input: {} }),
      ev("guardrail", "", { id: "g", message: "Refused", kind: "policy" }),
      ev("error", "Process crashed"),
      ev("file", "", { id: "f", path: "out.txt" }),
      ev("turn_done", "failed", { ok: false, result: "failed" }),
    ]);
    const turn = run(entries);
    expect(keys(turn.attention)).toEqual(["guardrail", "error", "turn"]);
    expect(keys(turn.answer)).toEqual(["file"]);
    expect(turn.status).toBe("error");
  });

  it("treats history that starts mid-turn as a turn of its own", () => {
    const entries = group([ev("tool", "", { id: "t", name: "Read", status: "ok", input: {} }), ev("message", "result", null), ev("prompt", "next")]);
    expect(entries.map((entry) => entry.type)).toEqual(["run", "item"]);
    expect(run(entries).key).toMatch(/^run:/);
  });

  it("gives each run a stable key that survives new activity", () => {
    const base = [ev("prompt", "go"), ev("tool", "", { id: "t", name: "Bash", status: "running", input: {} })];
    const before = run(group(base, ctx({ running: true }))).key;
    const after = run(group([...base, ev("message", "more")], ctx({ running: true }))).key;
    expect(after).toBe(before);
  });
});

describe("reasoningTitle", () => {
  it("prefers the bold title and strips markdown markers", () => {
    expect(reasoningTitle("**Planning bot design…**\n\nLonger text")).toBe("Planning bot design…");
    expect(reasoningTitle("Plain first line\nsecond")).toBe("Plain first line");
    expect(reasoningTitle("## Heading with `code`")).toBe("Heading with code");
    expect(reasoningTitle("   ")).toBe("Thinking");
    expect(reasoningTitle("x".repeat(200)).length).toBeLessThanOrEqual(80);
  });
});
