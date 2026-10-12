import { describe, expect, it } from "vitest";
import type { Definition, Journal, Step, StepType } from "./api";
import {
  ROOT, describeSchedule, earlierSteps, flatten, insertProblem, insertStep, laneKey, mapIssues, moveStep, newStep, nextId, removeStep, timeline, triggerSummary,
} from "./flow";

const block = (id: string, extra: Partial<Step> = {}): Step => ({ id, name: "", type: "notify", params: {}, when: "", timeoutSec: 300, retries: 0, backoffMs: 1000, onError: "stop", strategy: "at-most-once", ...extra });
const condition = (id: string, then: Step[] = [], otherwise: Step[] = []) => block(id, { type: "condition", params: { condition: "true" }, then, else: otherwise });
const ids = (steps: Step[]) => flatten(steps).map((at) => `${at.lane}:${at.step.id}`);

// a, check{then: [b, inner{then: [c]}], else: [d]}, e
const tree = () => [block("a"), condition("check", [block("b"), condition("inner", [block("c")])], [block("d")]), block("e")];

describe("tree paths and issues", () => {
  it("walks lanes in execution order with daemon paths", () => {
    expect(flatten(tree()).map((at) => `${at.path}=${at.step.id}@${at.depth}`)).toEqual([
      "steps.0=a@0", "steps.1=check@0", "steps.1.then.0=b@1", "steps.1.then.1=inner@1", "steps.1.then.1.then.0=c@2", "steps.1.else.0=d@1", "steps.2=e@0",
    ]);
  });

  it("maps nested issue paths to the block and field, and flow paths to the flow", () => {
    const map = mapIssues(tree(), [
      { path: "steps.1.then.1.then.0.params.prompt", message: "Required" },
      { path: "steps.1.else.0.id", message: "Duplicate step ID" },
      { path: "steps.1", message: "Add a block to at least one lane" },
      { path: "trigger.watch.repo", message: "Use owner/repository" },
      { path: "steps", message: "Use between 1 and 50 blocks" },
      { path: "steps.9.params.x", message: "Stale path" },
    ]);
    expect(map.steps).toEqual({ c: { "params.prompt": ["Required"] }, d: { id: ["Duplicate step ID"] }, check: { "": ["Add a block to at least one lane"] } });
    expect(map.flow).toEqual({ "trigger.watch.repo": ["Use owner/repository"], steps: ["Use between 1 and 50 blocks"], "steps.9.params.x": ["Stale path"] });
  });

  it("keeps an issue on its block after the block moves", () => {
    const steps = tree();
    const map = mapIssues(steps, [{ path: "steps.2.params.title", message: "Required" }]);
    const moved = moveStep(steps, "e", laneKey("check", "else"), 0)!;
    expect(flatten(moved).find((at) => at.step.id === "e")?.path).toBe("steps.1.else.0");
    expect(map.steps.e).toEqual({ "params.title": ["Required"] });
  });
});

describe("insert and move", () => {
  it("moves within a lane with final-index semantics", () => {
    const steps = [block("a"), block("b"), block("c")];
    expect(moveStep(steps, "a", ROOT, 2)!.map((s) => s.id)).toEqual(["b", "c", "a"]);
    expect(moveStep(steps, "c", ROOT, 0)!.map((s) => s.id)).toEqual(["c", "a", "b"]);
    expect(steps.map((s) => s.id)).toEqual(["a", "b", "c"]);
  });

  it("moves between lanes and the top level", () => {
    const steps = tree();
    const out = moveStep(steps, "c", ROOT, 0)!;
    expect(ids(out)).toEqual([`${ROOT}:c`, `${ROOT}:a`, `${ROOT}:check`, "check/then:b", "check/then:inner", "check/else:d", `${ROOT}:e`]);
    const back = moveStep(out, "a", laneKey("inner", "else"), 5)!;
    expect(ids(back)).toContain("inner/else:a");
  });

  it("refuses to move a block into its own lane or a lane nested in it", () => {
    const steps = tree();
    expect(moveStep(steps, "check", laneKey("check", "then"), 0)).toBeNull();
    expect(moveStep(steps, "check", laneKey("inner", "then"), 0)).toBeNull();
    expect(moveStep(steps, "inner", laneKey("inner", "else"), 0)).toBeNull();
  });

  it("enforces the nesting depth for inserts and moves", () => {
    // l1 > l2 > l3: blocks in l3's lanes are at depth 3, the deepest allowed.
    const steps = [condition("l1", [condition("l2", [condition("l3")])])];
    expect(insertProblem(steps, laneKey("l3", "then"))).toBeNull();
    expect(insertStep(steps, laneKey("l3", "then"), 0, block("x"))).not.toBeNull();
    const deepest = insertStep(steps, laneKey("l3", "then"), 0, condition("l4"))!;
    expect(insertProblem(deepest, laneKey("l4", "then"))).toMatch(/at most 3 deep/);
    expect(insertStep(deepest, laneKey("l4", "else"), 0, block("y"))).toBeNull();
    // A condition holding a lane cannot go where its lane would end up four deep.
    const top = [...steps, condition("big", [block("z")])];
    expect(moveStep(top, "big", laneKey("l3", "then"), 0)).toBeNull();
    expect(moveStep(top, "big", laneKey("l2", "then"), 0)).not.toBeNull();
  });

  it("refuses an insert whose ID is already used anywhere in the tree", () => {
    expect(insertStep(tree(), ROOT, 0, block("c"))).toBeNull();
    expect(insertProblem(tree(), ROOT, block("c"))).toMatch(/already uses/);
  });

  it("caps a flow at 50 blocks, lanes included", () => {
    const full = [condition("c0", Array.from({ length: 49 }, (_, i) => block(`n${i}`)))];
    expect(insertStep(full, ROOT, 1, block("extra"))).toBeNull();
    expect(insertProblem(removeStep(full, "n0"), ROOT)).toBeNull();
  });

  it("removes a block with everything in its lanes", () => {
    expect(ids(removeStep(tree(), "check"))).toEqual([`${ROOT}:a`, `${ROOT}:e`]);
  });
});

describe("new blocks", () => {
  const spec: StepType = { type: "bot.tell", label: "Ask a bot", group: "Bots", description: "", available: true, reason: "", sideEffect: true, keySupported: false, fields: [
    { key: "botId", label: "Bot", kind: "bot", required: true, options: [], hint: "" },
    { key: "wait", label: "Wait", kind: "boolean", required: false, options: [], hint: "" },
  ] };

  it("generates short IDs unused anywhere, lanes included", () => {
    const steps = [condition("check1", [block("ask1")], [block("ask2")])];
    expect(nextId(steps, "bot.tell")).toBe("ask3");
    expect(nextId(steps, "condition")).toBe("check2");
    expect(nextId([], "task.start")).toBe("task1");
  });

  it("fills defaults and gives conditions empty lanes", () => {
    expect(newStep(spec, [])).toMatchObject({ id: "ask1", type: "bot.tell", params: { wait: false }, strategy: "at-most-once" });
    expect(newStep({ ...spec, type: "condition", fields: [] }, [])).toMatchObject({ id: "check1", then: [], else: [] });
  });
});

describe("words", () => {
  it("describes common schedules and keeps others as written", () => {
    expect(describeSchedule("0 9 * * 1-5")).toBe("Every weekday at 09:00");
    expect(describeSchedule("30 2 * * *")).toBe("Every day at 02:30");
    expect(describeSchedule("every 15m")).toBe("Every 15 minutes");
    expect(describeSchedule("*/5 1 2 * *")).toBe("Schedule */5 1 2 * *");
  });

  it("summarises triggers", () => {
    const base = { type: "manual", schedule: "", catchUp: "skip", event: "", filter: "" };
    const topics = [{ topic: "agent.waiting", label: "Agent waiting for you", group: "Tasks", fields: [] }];
    expect(triggerSummary({ ...base, type: "event", event: "agent.waiting" }, topics)).toBe("When agent waiting for you");
    expect(triggerSummary({ ...base, type: "watch", watch: { source: "github", kind: "pr_opened", repo: "acme/web" } }, [])).toBe("On new pull requests in acme/web");
    expect(triggerSummary({ ...base, type: "watch", watch: { source: "mcp", kind: "deployment", tool: "list-deployments" } }, [])).toBe("On Railway deployments");
    expect(triggerSummary(base, [])).toBe("Manual");
  });

  it("offers outputs of blocks that surely ran before a block", () => {
    const steps = tree();
    expect(earlierSteps(steps, "c").map((s) => s.id)).toEqual(["a", "check", "b", "inner"]);
    expect(earlierSteps(steps, "e").map((s) => s.id)).toEqual(["a", "check"]);
    expect(earlierSteps(steps, "d").map((s) => s.id)).toEqual(["a", "check"]);
  });

  it("maps journal rows to blocks, hides joins and reads the lane a condition took", () => {
    const definition = { steps: tree() } as Definition;
    const row = (stepId: string, type: string, output: unknown = null): Journal => ({ runId: "r", stepId, seq: 0, type, status: "succeeded", attempt: 1, key: "", input: null, output, error: "", startedAt: 1, finishedAt: 2 });
    const rows = timeline(definition, [row("a", "notify"), row("check", "branch", { condition: false, next: "d" }), row("check:join", "branch"), row("d", "notify")]);
    expect(rows.map((r) => [r.entry.stepId, r.step?.id ?? null, r.lane, r.depth])).toEqual([["a", "a", null, 0], ["check", "check", "else", 0], ["d", "d", null, 1]]);
  });
});
