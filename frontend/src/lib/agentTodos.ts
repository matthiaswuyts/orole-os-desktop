import { isRecord, type FeedItem, type ToolCall } from "./taskEvents";

/**
 * Agents keep a written plan while they work, through their own todo tools: Claude Code's TodoWrite (or TaskCreate and
 * TaskUpdate), Codex's update_plan (the daemon reports turn/plan/updated as that call), Gemini CLI's write_todos and omp's
 * `todo` operations. This module folds those calls, in order, into one plan, and splits a run's other steps between the
 * plan step that was in progress when they ran and the work outside the plan.
 */

/** `backlog` is a pending step of a later phase (omp groups its list into phases); every other harness has one phase. */
export type AgentTodoStatus = "backlog" | "pending" | "in_progress" | "completed" | "failed" | "blocked" | "cancelled";

export interface AgentTodo {
  /** Stable across revisions: the harness's id, else the step's text. */
  id: string;
  /** What to do, in the imperative: "Run the tests". */
  content: string;
  /** What it is doing while in progress: "Running the tests". */
  activeForm?: string;
  status: AgentTodoStatus;
  /** Why a blocked or failed step stopped. */
  reason?: string;
  phase?: string;
}

export interface TodoPlan { todos: AgentTodo[]; explanation?: string }

const norm = (name: string) => name.toLowerCase().replace(/[^a-z0-9]/g, "");
const PLAN_TOOLS: Record<string, true> = { todowrite: true, updateplan: true, writetodos: true, taskcreate: true, taskupdate: true, todo: true };
const str = (v: unknown) => typeof v === "string" ? v.trim() : "";

/** A call that writes the agent's plan (and not, say, a TodoRead or a subagent `Task`). */
export const isPlanCall = (call: Pick<ToolCall, "name" | "server">) => !call.server && Object.hasOwn(PLAN_TOOLS, norm(call.name));

function status(value: unknown): AgentTodoStatus {
  switch (norm(str(value))) {
    case "inprogress": case "active": case "running": case "started": return "in_progress";
    case "completed": case "complete": case "done": return "completed";
    case "cancelled": case "canceled": case "abandoned": case "dropped": case "skipped": return "cancelled";
    case "failed": case "error": return "failed";
    case "blocked": return "blocked";
    default: return "pending";
  }
}

/** Gives repeated texts distinct ids so React rows and timings never collide. */
function uniqueIds(todos: AgentTodo[]): AgentTodo[] {
  const seen = new Map<string, number>();
  return todos.map((todo) => {
    const n = seen.get(todo.id) ?? 0;
    seen.set(todo.id, n + 1);
    return n ? { ...todo, id: `${todo.id}#${n + 1}` } : todo;
  });
}

/** A whole-list rewrite (TodoWrite, update_plan, write_todos): `items` with their text under `textKey`. */
function rewrite(items: unknown, textKey: string, explanation?: string): TodoPlan | null {
  if (!Array.isArray(items)) return null;
  const todos = items.filter(isRecord).flatMap((item): AgentTodo[] => {
    const content = str(item[textKey]) || str(item.content) || str(item.description) || str(item.step);
    if (!content) return [];
    const activeForm = str(item.activeForm);
    return [{ id: str(item.id) || content, content, status: status(item.status), ...(activeForm && activeForm !== content ? { activeForm } : {}) }];
  });
  return { todos: uniqueIds(todos), ...(explanation ? { explanation } : {}) };
}

// ---- omp's `todo` operations ----

const ompPhases = (input: Record<string, unknown>): { phase: string; items: string[] }[] => {
  if (Array.isArray(input.list)) return input.list.filter(isRecord).map((p) => ({ phase: str(p.phase), items: Array.isArray(p.items) ? p.items.map(str).filter(Boolean) : [] }));
  return Array.isArray(input.items) ? [{ phase: str(input.phase), items: input.items.map(str).filter(Boolean) }] : [];
};
const ompTodo = (content: string, phase: string): AgentTodo => ({ id: phase ? `${phase}/${content}` : content, content, status: "pending", ...(phase ? { phase } : {}) });
const open = (t: AgentTodo) => t.status === "pending" || t.status === "backlog";

/** Applies one omp `todo` op: tasks are named by their verbatim text; after any change one task is active. */
function ompOp(prev: TodoPlan | null, input: unknown): TodoPlan | null {
  if (!isRecord(input)) return prev;
  const op = str(input.op);
  let todos: AgentTodo[] = (prev?.todos ?? []).map((t) => t.status === "backlog" ? { ...t, status: "pending" } : t);
  const task = str(input.task), phase = str(input.phase), reason = str(input.reason);
  const hits = (t: AgentTodo) => task ? t.content === task && (!phase || t.phase === phase) : phase ? t.phase === phase : false;
  const set = (match: (t: AgentTodo) => boolean, next: Partial<AgentTodo>) => { todos = todos.map((t) => match(t) ? { ...t, ...next } : t); };
  switch (op) {
    case "init": todos = ompPhases(input).flatMap((p) => p.items.map((item) => ompTodo(item, p.phase))); break;
    case "append": {
      const lastPhase = todos[todos.length - 1]?.phase ?? "";
      todos = [...todos, ...ompPhases(input).flatMap((p) => p.items.map((item) => ompTodo(item, p.phase || lastPhase)))];
      break;
    }
    case "start":
      set((t) => t.status === "in_progress" && !hits(t), { status: "pending" });
      set(hits, { status: "in_progress" });
      break;
    case "done": set(task || phase ? hits : (t) => t.status === "in_progress", { status: "completed" }); break;
    case "drop": set(hits, { status: "cancelled" }); break;
    case "block": set(hits, { status: "blocked", ...(reason ? { reason } : {}) }); break;
    case "unblock": set(hits, { status: "pending", reason: undefined }); break;
    case "rm": todos = todos.filter((t) => !hits(t)); break;
    default: return prev; // view, or an op this build does not know
  }
  // One task stays active: the earliest of several, else the earliest pending one.
  const active = todos.findIndex((t) => t.status === "in_progress");
  if (active === -1) { const next = todos.findIndex(open); if (next !== -1) todos[next] = { ...todos[next]!, status: "in_progress" }; }
  else todos = todos.map((t, i) => i > active && t.status === "in_progress" ? { ...t, status: "pending" } : t);
  const current = todos.find((t) => t.status === "in_progress" || open(t))?.phase;
  todos = todos.map((t) => t.status === "pending" && t.phase !== current ? { ...t, status: "backlog" } : t);
  return { todos: uniqueIds(todos) };
}

// ---- Claude Code's incremental TaskCreate / TaskUpdate ----

function taskCreate(prev: TodoPlan | null, call: ToolCall): TodoPlan | null {
  const input = isRecord(call.input) ? call.input : {};
  const content = str(input.subject) || str(input.content);
  if (!content) return prev;
  const todos = prev?.todos ?? [];
  // Claude numbers tasks from 1 and says so in the result ("Task #3 created"); before the result arrives, the next number.
  const reported = /#(\d+)/.exec(call.output)?.[1];
  const id = reported ?? String(todos.reduce((max, t) => Math.max(max, Number(t.id) || 0), 0) + 1);
  const activeForm = str(input.activeForm);
  return { ...prev, todos: [...todos.filter((t) => t.id !== id), { id, content, status: "pending", ...(activeForm ? { activeForm } : {}) }] };
}

function taskUpdate(prev: TodoPlan | null, input: unknown): TodoPlan | null {
  if (!prev || !isRecord(input)) return prev;
  const id = str(input.taskId) || str(input.id);
  if (!prev.todos.some((t) => t.id === id)) return prev;
  if (norm(str(input.status)) === "deleted") return { ...prev, todos: prev.todos.filter((t) => t.id !== id) };
  return {
    ...prev,
    todos: prev.todos.map((t) => t.id !== id ? t : {
      ...t,
      ...(str(input.status) ? { status: status(input.status) } : {}),
      ...(str(input.subject) ? { content: str(input.subject) } : {}),
      ...(str(input.activeForm) ? { activeForm: str(input.activeForm) } : {}),
    }),
  };
}

/** The plan after one plan call; `prev` unchanged when the call carries nothing usable. */
export function foldPlanCall(prev: TodoPlan | null, call: ToolCall): TodoPlan | null {
  if (call.status === "error" || !isPlanCall(call)) return prev;
  const input = isRecord(call.input) ? call.input : {};
  switch (norm(call.name)) {
    case "todowrite": return rewrite(input.todos, "content") ?? prev;
    case "writetodos": return rewrite(input.todos, "description") ?? prev;
    case "updateplan": return rewrite(input.plan, "step", str(input.explanation)) ?? prev;
    case "taskcreate": return taskCreate(prev, call);
    case "taskupdate": return taskUpdate(prev, call.input);
    case "todo": return ompOp(prev, call.input);
    default: return prev;
  }
}

export interface TodoTiming { start: number; end?: number }

/** One run's steps arranged around its plan. */
export interface PlannedRun {
  /** The latest plan of the run; null when the agent kept none. */
  plan: TodoPlan | null;
  /** Steps outside any plan step: everything before the plan, between steps, and failed plan calls. Plan calls themselves are gone. */
  loose: FeedItem[];
  /** The steps that ran while each plan step was in progress, by todo id. */
  work: ReadonlyMap<string, readonly FeedItem[]>;
  /** When each step went in progress and, once settled, when it finished (Unix ms). */
  timing: ReadonlyMap<string, TodoTiming>;
}

const SETTLED: Partial<Record<AgentTodoStatus, true>> = { completed: true, failed: true, cancelled: true, blocked: true };

export function planRun(steps: readonly FeedItem[]): PlannedRun {
  let plan: TodoPlan | null = null;
  const loose: FeedItem[] = [];
  const work = new Map<string, FeedItem[]>();
  const timing = new Map<string, TodoTiming>();
  for (const item of steps) {
    if (item.type === "tool" && isPlanCall(item.call)) {
      const next = foldPlanCall(plan, item.call);
      if (next === plan) { loose.push(item); continue; } // a failed or unreadable plan write stays visible as a step
      plan = next;
      const at = Date.parse(item.at);
      for (const todo of plan?.todos ?? []) {
        const known = timing.get(todo.id);
        if (todo.status === "in_progress" && (!known || known.end !== undefined)) timing.set(todo.id, { start: at });
        else if (SETTLED[todo.status] && known && known.end === undefined) timing.set(todo.id, { ...known, end: at });
      }
      continue;
    }
    const active = plan?.todos.find((t) => t.status === "in_progress");
    if (!active) { loose.push(item); continue; }
    const list = work.get(active.id);
    if (list) list.push(item); else work.set(active.id, [item]);
  }
  return { plan: plan && plan.todos.length ? plan : null, loose, work, timing };
}
