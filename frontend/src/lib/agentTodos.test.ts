import { describe, expect, it } from "vitest";
import { foldPlanCall, planRun, type TodoPlan } from "./agentTodos";
import type { FeedItem, ToolCall } from "./taskEvents";

let seq = 0;
const at = (s: number) => new Date(Date.UTC(2026, 9, 8, 12, 0, s)).toISOString();
const call = (name: string, input: unknown, patch: Partial<ToolCall> = {}): ToolCall => ({ id: `c${++seq}`, linked: true, name, server: "", status: "ok", input, output: "", summary: "", ...patch });
const tool = (c: ToolCall, sec: number): FeedItem => ({ type: "tool", key: String(++seq), at: at(sec), call: c });
const fold = (calls: ToolCall[]) => calls.reduce<TodoPlan | null>(foldPlanCall, null);
const states = (plan: TodoPlan | null) => plan?.todos.map((t) => `${t.content}:${t.status}`);

describe("foldPlanCall", () => {
  it("reads each harness's whole-list rewrite, keeping activeForm and Codex's explanation", () => {
    const claude = fold([call("TodoWrite", { todos: [{ content: "Run the tests", activeForm: "Running the tests", status: "in_progress" }, { content: "Ship", status: "pending" }] })]);
    expect(states(claude)).toEqual(["Run the tests:in_progress", "Ship:pending"]);
    expect(claude?.todos[0]?.activeForm).toBe("Running the tests");
    const codex = fold([call("update_plan", { explanation: "Fix, then test", plan: [{ step: "Fix", status: "inProgress" }, { step: "Test", status: "completed" }] })]);
    expect(states(codex)).toEqual(["Fix:in_progress", "Test:completed"]);
    expect(codex?.explanation).toBe("Fix, then test");
    expect(states(fold([call("write_todos", { todos: [{ description: "Read", status: "cancelled" }] })]))).toEqual(["Read:cancelled"]);
  });

  it("ignores failed calls, MCP tools of the same name and lists it cannot read", () => {
    expect(fold([call("TodoWrite", { todos: [{ content: "x", status: "pending" }] }, { status: "error" })])).toBeNull();
    expect(fold([call("todo", { op: "init", items: ["x"] }, { server: "notes" })])).toBeNull();
    const plan = fold([call("TodoWrite", { todos: [{ content: "keep", status: "pending" }] }), call("TodoWrite", { todos: "garbage" })]);
    expect(states(plan)).toEqual(["keep:pending"]);
  });

  it("builds Claude's TaskCreate/TaskUpdate list by task number, and deletes on status deleted", () => {
    const plan = fold([
      call("TaskCreate", { subject: "Find the bug" }, { output: "Task #1 created successfully" }),
      call("TaskCreate", { subject: "Fix it", activeForm: "Fixing it" }), // result not in yet: next number
      call("TaskUpdate", { taskId: "2", status: "in_progress" }),
      call("TaskUpdate", { taskId: "1", status: "deleted" }),
      call("TaskUpdate", { taskId: "9", status: "completed" }),
    ]);
    expect(plan?.todos.map((t) => [t.id, t.content, t.status])).toEqual([["2", "Fix it", "in_progress"]]);
  });

  it("replays omp's todo ops: one active task, phases after the current one are backlog", () => {
    const init = call("todo", { op: "init", list: [{ phase: "Build", items: ["a", "b"] }, { phase: "Ship", items: ["c"] }] });
    expect(states(fold([init]))).toEqual(["a:in_progress", "b:pending", "c:backlog"]);
    expect(states(fold([init, call("todo", { op: "done", task: "a" })]))).toEqual(["a:completed", "b:in_progress", "c:backlog"]);
    expect(states(fold([init, call("todo", { op: "done", phase: "Build" })]))).toEqual(["a:completed", "b:completed", "c:in_progress"]);
    const blocked = fold([init, call("todo", { op: "block", task: "a", reason: "needs a key" })]);
    expect(states(blocked)).toEqual(["a:blocked", "b:in_progress", "c:backlog"]);
    expect(blocked?.todos[0]?.reason).toBe("needs a key");
    expect(states(fold([init, call("todo", { op: "start", task: "b" })]))).toEqual(["a:pending", "b:in_progress", "c:backlog"]);
    expect(states(fold([init, call("todo", { op: "append", phase: "Ship", items: ["d"] }), call("todo", { op: "rm", task: "b" })]))).toEqual(["a:in_progress", "c:backlog", "d:backlog"]);
    expect(states(fold([init, call("todo", { op: "view" })]))).toEqual(["a:in_progress", "b:pending", "c:backlog"]);
  });
});

describe("planRun", () => {
  it("hangs each step on the plan step in progress when it ran, times plan steps and keeps the rest loose", () => {
    const before = tool(call("Read", { file_path: "a.ts" }), 1);
    const write1 = tool(call("TodoWrite", { todos: [{ content: "Find", status: "in_progress" }, { content: "Fix", status: "pending" }] }), 2);
    const find = tool(call("Grep", { pattern: "x" }), 3);
    const write2 = tool(call("TodoWrite", { todos: [{ content: "Find", status: "completed" }, { content: "Fix", status: "in_progress" }] }), 10);
    const fix = tool(call("Edit", { file_path: "a.ts" }), 11);
    const failed = tool(call("TodoWrite", {}, { status: "error" }), 12);
    const run = planRun([before, write1, find, write2, fix, failed]);
    expect(states(run.plan)).toEqual(["Find:completed", "Fix:in_progress"]);
    expect(run.loose).toEqual([before, failed]);
    expect(run.work.get("Find")).toEqual([find]);
    expect(run.work.get("Fix")).toEqual([fix]);
    expect(run.timing.get("Find")).toEqual({ start: Date.parse(at(2)), end: Date.parse(at(10)) });
    expect(run.timing.get("Fix")).toEqual({ start: Date.parse(at(10)) });
  });

  it("has no plan when the run never wrote one", () => {
    const step = tool(call("Read", {}), 1);
    expect(planRun([step])).toMatchObject({ plan: null, loose: [step] });
  });
});
