// Pure operations on a flow: the step tree (lanes of condition blocks), daemon issue paths,
// and the words the builder shows for triggers, blocks and run journals.
import type { Definition, Issue, Journal, Step, StepType, Topic, Trigger } from "./api";

/** Mirrors the daemon (internal/automations/flow.go): blocks per flow, lanes included, and lane nesting. */
export const MAX_BLOCKS = 50;
export const MAX_DEPTH = 3;
/** The top-level lane. Other lanes are `<conditionId>/then` and `<conditionId>/else`; a step ID cannot contain "/". */
export const ROOT = "root";
export type Lane = "then" | "else";
export const LANES: Lane[] = ["then", "else"];
export const laneKey = (conditionId: string, lane: Lane) => `${conditionId}/${lane}`;
export const LANE_LABEL: Record<Lane, string> = { then: "If true", else: "Otherwise" };

export type Params = Record<string, unknown>;
export function params(step: Step): Params {
  const p = step.params;
  return p && typeof p === "object" && !Array.isArray(p) ? p as Params : {};
}

export interface Located { step: Step; lane: string; index: number; depth: number; path: string }

/** Visits every block in execution order (a condition before its lanes) with its daemon path. */
export function walk(steps: Step[], visit: (at: Located) => void, lane = ROOT, depth = 0, path = "steps") {
  steps.forEach((step, index) => {
    const at = `${path}.${index}`;
    visit({ step, lane, index, depth, path: at });
    for (const l of LANES) walk(step[l] ?? [], visit, laneKey(step.id, l), depth + 1, `${at}.${l}`);
  });
}

export function flatten(steps: Step[]): Located[] {
  const out: Located[] = [];
  walk(steps, (at) => out.push(at));
  return out;
}

export function locate(steps: Step[], id: string): Located | null {
  return flatten(steps).find((at) => at.step.id === id) ?? null;
}

/** The blocks of a lane, or null when the lane does not exist. */
export function laneSteps(steps: Step[], lane: string): Step[] | null {
  if (lane === ROOT) return steps;
  const [id, which] = lane.split("/") as [string, Lane];
  const owner = locate(steps, id)?.step;
  return owner?.type === "condition" && LANES.includes(which) ? owner[which] ?? [] : null;
}

/** Nesting level of the blocks in a lane: 0 at the top, 1 inside a top-level condition... */
export function laneDepth(steps: Step[], lane: string): number | null {
  if (lane === ROOT) return 0;
  const owner = locate(steps, lane.split("/")[0]!);
  return owner ? owner.depth + 1 : null;
}

/** How many lane levels a block brings along (0 for a block without nested blocks). */
function height(step: Step): number {
  return Math.max(0, ...LANES.flatMap((l) => (step[l] ?? []).map((child) => 1 + height(child))));
}

const ids = (steps: Step[]) => flatten(steps).map((at) => at.step.id);

/** Replaces one lane through `change`; untouched branches keep their identity. */
function mapLane(steps: Step[], lane: string, change: (lane: Step[]) => Step[]): Step[] {
  if (lane === ROOT) return change(steps);
  let changed = false;
  const next = steps.map((step) => {
    let out = step;
    for (const l of LANES) {
      const children = step[l];
      const updated = laneKey(step.id, l) === lane && step.type === "condition" ? change(children ?? []) : children && mapLane(children, lane, change);
      if (updated !== children) out = { ...out, [l]: updated };
    }
    if (out !== step) changed = true;
    return out;
  });
  return changed ? next : steps;
}

export function updateStep(steps: Step[], id: string, change: (step: Step) => Step): Step[] {
  let changed = false;
  const next = steps.map((step) => {
    let out = step.id === id ? change(step) : step;
    for (const l of LANES) {
      const children = out[l];
      const updated = children && updateStep(children, id, change);
      if (updated !== children) out = { ...out, [l]: updated };
    }
    if (out !== step) changed = true;
    return out;
  });
  return changed ? next : steps;
}

export function removeStep(steps: Step[], id: string): Step[] {
  let changed = false;
  const next: Step[] = [];
  for (const step of steps) {
    if (step.id === id) { changed = true; continue; }
    let out = step;
    for (const l of LANES) {
      const children = out[l];
      const updated = children && removeStep(children, id);
      if (updated !== children) out = { ...out, [l]: updated };
    }
    if (out !== step) changed = true;
    next.push(out);
  }
  return changed ? next : steps;
}

/** Why `block` (default: one new block) cannot go into `lane`, or null when it can. */
export function insertProblem(steps: Step[], lane: string, block?: Step): string | null {
  const depth = laneDepth(steps, lane);
  if (depth === null || laneSteps(steps, lane) === null) return "This lane no longer exists.";
  if (depth + (block ? height(block) : 0) > MAX_DEPTH) return `Conditions can nest at most ${MAX_DEPTH} deep.`;
  const incoming = block ? ids([block]) : [];
  if (flatten(steps).length + Math.max(1, incoming.length) > MAX_BLOCKS) return `A flow holds at most ${MAX_BLOCKS} blocks.`;
  const taken = new Set(ids(steps));
  if (incoming.some((id) => taken.has(id))) return "Another block already uses this ID.";
  return null;
}

/** Inserts `block` at `index` of `lane`; null when the limits or IDs forbid it. */
export function insertStep(steps: Step[], lane: string, index: number, block: Step): Step[] | null {
  if (insertProblem(steps, lane, block)) return null;
  return mapLane(steps, lane, (list) => {
    const at = Math.max(0, Math.min(index, list.length));
    return [...list.slice(0, at), block, ...list.slice(at)];
  });
}

/**
 * Moves a block (with its lanes) so it ends up at `index` of `lane`. Null when the target is the
 * block's own lane or one nested in it, or when the move would nest conditions too deep.
 */
export function moveStep(steps: Step[], id: string, lane: string, index: number): Step[] | null {
  const from = locate(steps, id);
  if (!from) return null;
  if (lane !== ROOT) {
    const owner = lane.split("/")[0]!;
    if (owner === id || ids([from.step]).includes(owner)) return null;
  }
  const without = removeStep(steps, id);
  const depth = laneDepth(without, lane);
  if (depth === null || laneSteps(without, lane) === null || depth + height(from.step) > MAX_DEPTH) return null;
  return mapLane(without, lane, (list) => {
    const at = Math.max(0, Math.min(index, list.length));
    return [...list.slice(0, at), from.step, ...list.slice(at)];
  });
}

const ID_PREFIX: Record<string, string> = {
  "bot.tell": "ask", "task.start": "task", "task.prompt": "followup", "task.wait": "wait", notify: "notify", "http.request": "webhook",
  "github.query": "github", "mcp.tool": "tool", condition: "check", delay: "delay", approval: "approve", "command.run": "cmd", "test.run": "test",
};

/** A short ID (`ask1`, `task2`...) unused anywhere in the tree, lanes included. */
export function nextId(steps: Step[], type: string): string {
  const prefix = ID_PREFIX[type] ?? (type.replace(/[^A-Za-z]/g, "").slice(0, 8) || "block");
  const taken = new Set(ids(steps));
  let n = 1;
  while (taken.has(`${prefix}${n}`)) n++;
  return `${prefix}${n}`;
}

export function newStep(spec: StepType, steps: Step[]): Step {
  const defaults: Params = {};
  for (const field of spec.fields ?? []) {
    if (field.kind === "enum" && field.options?.length) defaults[field.key] = field.options[0];
    if (field.kind === "boolean") defaults[field.key] = false;
  }
  return {
    id: nextId(steps, spec.type), name: "", type: spec.type, params: defaults, when: "", timeoutSec: 300, retries: 0, backoffMs: 1000,
    onError: "stop", strategy: "at-most-once", ...(spec.type === "condition" ? { then: [], else: [] } : {}),
  };
}

/** Daemon issues placed where the builder shows them: per block (by ID, then field path) or per flow path. */
export interface IssueMap { flow: Record<string, string[]>; steps: Record<string, Record<string, string[]>> }
const STEP_PATH = /^steps\.\d+(?:\.(?:then|else)\.\d+)*/;

/**
 * Maps issue paths (`steps.1.then.0.params.prompt`) to the blocks of the tree they were reported
 * for. Keying by block ID keeps an issue on its block when blocks are reordered afterwards.
 */
export function mapIssues(steps: Step[], issues: Issue[]): IssueMap {
  const byPath = new Map(flatten(steps).map((at) => [at.path, at.step.id]));
  const out: IssueMap = { flow: {}, steps: {} };
  for (const { path, message } of issues ?? []) {
    const match = STEP_PATH.exec(path);
    const id = match && byPath.get(match[0]);
    if (match && id) {
      const field = path.slice(match[0].length + 1);
      ((out.steps[id] ??= {})[field] ??= []).push(message);
    } else {
      (out.flow[path] ??= []).push(message);
    }
  }
  return out;
}

export const issueCount = (map: IssueMap, id: string) => Object.values(map.steps[id] ?? {}).reduce((n, list) => n + list.length, 0);

/** Event data fields a watch trigger delivers (internal/daemon/automation_watch.go). */
const WATCH_FIELDS: Record<string, string[]> = {
  push: ["repo", "branch", "sha", "url", "author", "message", "title"],
  pr_opened: ["repo", "number", "title", "url", "author", "head", "base", "state", "draft", "merged"],
  pr_merged: ["repo", "number", "title", "url", "author", "head", "base", "state", "draft", "merged"],
  workflow_failed: ["repo", "runId", "name", "title", "url", "branch", "sha", "event", "conclusion", "actor"],
  issue_opened: ["repo", "number", "title", "url", "author", "state"],
  deployment: ["id", "state", "server", "tool"],
};

/** `event.data.*` fields available to filters and templates for the flow's trigger. */
export function eventFields(trigger: Trigger, topics: Topic[]): { name: string; label: string }[] {
  if (trigger.type === "watch") return (WATCH_FIELDS[trigger.watch?.kind ?? ""] ?? []).map((name) => ({ name, label: name }));
  if (trigger.type !== "event" || !trigger.event) return [];
  const prefix = trigger.event.endsWith(".*") ? trigger.event.slice(0, -1) : null;
  const seen = new Map<string, string>();
  for (const topic of topics) {
    if (prefix ? !topic.topic.startsWith(prefix) : topic.topic !== trigger.event) continue;
    for (const field of topic.fields ?? []) if (!seen.has(field.name)) seen.set(field.name, field.label);
  }
  return [...seen].map(([name, label]) => ({ name, label }));
}

/**
 * Blocks whose output exists whenever `id` runs: earlier blocks of its lane and of every
 * enclosing lane, and the enclosing conditions. Lane blocks of an earlier condition are left
 * out because only one lane ran.
 */
export function earlierSteps(steps: Step[], id: string): Step[] {
  const out: Step[] = [];
  const visit = (lane: Step[]): boolean => {
    for (const step of lane) {
      if (step.id === id) return true;
      out.push(step);
      const mark = out.length;
      for (const l of LANES) {
        if (visit(step[l] ?? [])) return true;
        out.length = mark;
      }
    }
    return false;
  };
  return visit(steps) ? out : [];
}

export interface Names { bot: (id: string) => string | undefined; project: (id: string) => string | undefined }

/** One line about what a block does, from its key parameters. */
export function blockSummary(step: Step, names: Names): string {
  const p = params(step);
  const text = (key: string) => typeof p[key] === "string" ? (p[key] as string).trim() : "";
  const join = (...parts: string[]) => parts.filter(Boolean).join(" · ");
  switch (step.type) {
    case "bot.tell": return join(text("botId") ? names.bot(text("botId")) ?? text("botId") : "No bot chosen", text("message"));
    case "task.start": return join(text("projectId") ? names.project(text("projectId")) ?? text("projectId") : "No project chosen", text("title") || text("prompt"));
    case "task.prompt": return text("prompt");
    case "task.wait": return text("until") === "attention" ? "Until it finishes or needs you" : "Until it finishes";
    case "notify": return text("title");
    case "http.request": return join(text("method"), text("url"));
    case "github.query": return join(text("kind"), text("repo"));
    case "mcp.tool": return join(text("server"), text("tool"));
    case "condition": return text("condition") || "No condition yet";
    case "delay": return typeof p.minutes === "number" && p.minutes > 0 ? `${p.minutes} min` : "";
    case "approval": return text("message");
    case "command.run": case "test.run": return [text("executable"), ...(Array.isArray(p.args) ? p.args.map(String) : [])].join(" ").trim();
    default: return "";
  }
}

const DAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
const UNITS: Record<string, string> = { m: "minute", h: "hour", d: "day" };

/** Common schedules in words; anything else is shown as written. */
export function describeSchedule(raw: string): string {
  const s = raw.trim();
  if (!s) return "Schedule not set";
  const every = /^every\s+(\d+)\s*([mhd])$/i.exec(s);
  if (every) {
    const n = Number(every[1]);
    const unit = UNITS[every[2]!.toLowerCase()]!;
    return n === 1 ? `Every ${unit}` : `Every ${n} ${unit}s`;
  }
  const alias: Record<string, string> = { "@hourly": "Every hour", "@daily": "Every day at 00:00", "@midnight": "Every day at 00:00", "@weekly": "Every Sunday at 00:00", "@monthly": "Monthly on day 1 at 00:00" };
  if (alias[s.toLowerCase()]) return alias[s.toLowerCase()]!;
  const parts = s.split(/\s+/);
  if (parts.length === 5 && /^\d+$/.test(parts[0]!) && parts[2] === "*" && parts[3] === "*") {
    const [minute, hour, , , dow] = parts as [string, string, string, string, string];
    const mm = minute.padStart(2, "0");
    if (hour === "*" && dow === "*") return `Every hour at :${mm}`;
    if (/^\d+$/.test(hour)) {
      const at = `${hour.padStart(2, "0")}:${mm}`;
      if (dow === "*") return `Every day at ${at}`;
      if (dow === "1-5") return `Every weekday at ${at}`;
      if (dow === "0,6" || dow === "6,0") return `Every weekend day at ${at}`;
      if (/^[0-7]$/.test(dow)) return `Every ${DAYS[Number(dow) % 7]} at ${at}`;
    }
  }
  return `Schedule ${s}`;
}

const GITHUB_WATCH: Record<string, string> = {
  push: "On pushes to", pr_opened: "On new pull requests in", pr_merged: "On merged pull requests in",
  workflow_failed: "On failed workflows in", issue_opened: "On new issues in",
};
/** Known integration tools that list deployments. */
export const DEPLOYMENT_PRESETS = [
  { id: "vercel", label: "Vercel", tool: "list_deployments", args: { projectId: "" } },
  { id: "railway", label: "Railway", tool: "list-deployments", args: { projectId: "", environmentId: "", serviceId: "" } },
] as const;

/** What starts the flow, in words. */
export function triggerSummary(trigger: Trigger, topics: Topic[]): string {
  switch (trigger.type) {
    case "manual": return "Manual";
    case "webhook": return "On a local webhook";
    case "schedule": return describeSchedule(trigger.schedule);
    case "event": {
      if (!trigger.event) return "On an app event (not chosen)";
      const topic = topics.find((t) => t.topic === trigger.event);
      if (topic) return `When ${topic.label.charAt(0).toLowerCase()}${topic.label.slice(1)}`;
      return trigger.event.endsWith(".*") ? `On any ${trigger.event.slice(0, -2)} event` : `On ${trigger.event}`;
    }
    case "watch": {
      const w = trigger.watch;
      if (!w) return "Watch (not set up)";
      if (w.source === "github") {
        const branch = w.branch && (w.kind === "push" || w.kind === "workflow_failed") ? ` (${w.branch})` : "";
        return `${GITHUB_WATCH[w.kind] ?? "On changes in"} ${w.repo || "a repository"}${branch}`;
      }
      const preset = DEPLOYMENT_PRESETS.find((p) => p.tool === w.tool);
      return preset ? `On ${preset.label} deployments` : `On deployments from ${w.tool || "an integration"}`;
    }
    default: return trigger.type;
  }
}

/** "just now", "5m ago", "in 3h", "2d ago". */
export function relativeTime(ms: number, now = Date.now()): string {
  const s = Math.round((ms - now) / 1000);
  const abs = Math.abs(s);
  if (abs < 45) return s > 0 ? "in a moment" : "just now";
  const value = abs < 3600 ? `${Math.max(1, Math.round(abs / 60))}m` : abs < 86400 ? `${Math.round(abs / 3600)}h` : `${Math.round(abs / 86400)}d`;
  return s > 0 ? `in ${value}` : `${value} ago`;
}

export function duration(start: number, finish: number, now = Date.now()): string {
  if (!start) return "Not started";
  const seconds = Math.max(0, Math.round(((finish || now) - start) / 1000));
  const text = seconds < 60 ? `${seconds}s` : seconds < 3600 ? `${Math.floor(seconds / 60)}m ${seconds % 60}s` : `${Math.floor(seconds / 3600)}h ${Math.floor(seconds / 60) % 60}m`;
  return finish ? text : `${text} elapsed`;
}

export interface TimelineRow { entry: Journal; step: Step | null; depth: number; lane: Lane | null }

/**
 * Journal rows mapped to the run's blocks. The daemon journals a condition as a `branch` row
 * under the condition's ID with output.condition; `<id>:join` rows are internal and hidden.
 */
export function timeline(definition: Definition | undefined, entries: Journal[]): TimelineRow[] {
  const blocks = new Map(flatten(definition?.steps ?? []).map((at) => [at.step.id, at]));
  return (entries ?? []).filter((entry) => !entry.stepId.endsWith(":join")).map((entry) => {
    const at = blocks.get(entry.stepId);
    const output = entry.output && typeof entry.output === "object" ? entry.output as Params : {};
    const lane = entry.type === "branch" && typeof output.condition === "boolean" ? output.condition ? "then" : "else" : null;
    return { entry, step: at?.step ?? null, depth: at?.depth ?? 0, lane };
  });
}
