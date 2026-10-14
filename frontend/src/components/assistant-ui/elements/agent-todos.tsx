/*
 * Orole's own agent plan list, modelled on the behaviour HextaUI documents for its Agent Todos block
 * (https://hextaui.com/blocks/agent-todos): header with a done count, a progress bar, the plan's explanation, one line per
 * step with its status mark and timing, and the work behind each step one disclosure away. Nothing is copied.
 */
import { ChevronDownIcon } from "lucide-react";
import { useState, type ReactNode } from "react";
import { Collapsible, CollapsiblePanel, CollapsibleTrigger } from "@/components/ui/collapsible";
import type { AgentTodo, AgentTodoStatus } from "@/lib/agentTodos";
import { cn } from "@/lib/utils";
import { formatElapsed, useNow } from "./working-status";

export interface AgentTodoRow extends AgentTodo {
  /** When the step went in progress and when it settled (Unix ms). */
  startedAt?: number;
  endedAt?: number;
  /** The work done under the step, opened from its row. */
  details?: ReactNode;
  /** The step's latest piece of work, shown under it while it is in progress. */
  latest?: ReactNode;
}

const STATUS_LABEL: Record<AgentTodoStatus, string> = {
  backlog: "Backlog", pending: "To do", in_progress: "In progress", completed: "Done", failed: "Failed", blocked: "Blocked", cancelled: "Cancelled",
};

/** The status mark: an outlined ring to do, a dashed one for backlog, a turning arc in progress, a filled check when done. */
export function TodoMark({ status, spinning = false, className }: { status: AgentTodoStatus; spinning?: boolean; className?: string }) {
  const ring = <circle cx="8" cy="8" r="6.25" fill="none" stroke="currentColor" strokeWidth="1.5" />;
  const base = cn("size-4 shrink-0", className);
  switch (status) {
    case "completed": return <svg aria-hidden viewBox="0 0 16 16" className={cn(base, "text-success")}>
      <circle cx="8" cy="8" r="7" fill="currentColor" />
      <path d="M5.1 8.2 7 10l3.9-4" fill="none" stroke="var(--background)" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
    </svg>;
    case "in_progress": return <svg aria-hidden viewBox="0 0 16 16" className={cn(base, "text-info-foreground", spinning && "animate-spin [animation-duration:900ms] motion-reduce:animate-none")}>
      <circle cx="8" cy="8" r="6.25" fill="none" stroke="currentColor" strokeOpacity="0.25" strokeWidth="1.5" />
      <path d="M8 1.75a6.25 6.25 0 0 1 6.25 6.25" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>;
    case "backlog": return <svg aria-hidden viewBox="0 0 16 16" className={cn(base, "text-muted-foreground/60")}>
      <circle cx="8" cy="8" r="6.25" fill="none" stroke="currentColor" strokeWidth="1.5" strokeDasharray="2.2 2.7" />
    </svg>;
    case "failed": return <svg aria-hidden viewBox="0 0 16 16" className={cn(base, "text-destructive-foreground")}>
      {ring}<path d="m5.9 5.9 4.2 4.2m0-4.2-4.2 4.2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>;
    case "blocked": return <svg aria-hidden viewBox="0 0 16 16" className={cn(base, "text-warning-foreground")}>
      {ring}<path d="M8 4.9v3.6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" /><circle cx="8" cy="10.9" r="0.85" fill="currentColor" />
    </svg>;
    case "cancelled": return <svg aria-hidden viewBox="0 0 16 16" className={cn(base, "text-muted-foreground/60")}>
      {ring}<path d="M5.4 8h5.2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>;
    default: return <svg aria-hidden viewBox="0 0 16 16" className={cn(base, "text-muted-foreground")}>{ring}</svg>;
  }
}

function elapsed(todo: AgentTodoRow, now: number, running: boolean): string | null {
  if (todo.startedAt === undefined) return null;
  const end = todo.endedAt ?? (todo.status === "in_progress" && running ? now : undefined);
  if (end === undefined) return null;
  const seconds = Math.floor((end - todo.startedAt) / 1000);
  return seconds >= 1 ? formatElapsed(seconds) : null;
}

const TEXT_TONE: Record<AgentTodoStatus, string> = {
  backlog: "text-muted-foreground", pending: "text-foreground/90", in_progress: "text-info-foreground", completed: "text-muted-foreground",
  failed: "text-foreground/90", blocked: "text-foreground/90", cancelled: "text-muted-foreground line-through",
};

function TodoRow({ todo, running, now }: { todo: AgentTodoRow; running: boolean; now: number }) {
  const [open, setOpen] = useState(false);
  const active = todo.status === "in_progress";
  const text = active && todo.activeForm ? todo.activeForm : todo.content;
  const time = elapsed(todo, now, running);
  const line = <>
    <TodoMark status={todo.status} spinning={active && running} />
    <span className={cn("min-w-0 flex-1 truncate", TEXT_TONE[todo.status], active && running && "font-medium")}>
      <span className="sr-only">{STATUS_LABEL[todo.status]}: </span>{text}
    </span>
    {time && <span className="shrink-0 font-mono text-[11px] tabular-nums text-muted-foreground">{time}</span>}
  </>;
  const reason = todo.reason && (todo.status === "failed" || todo.status === "blocked")
    ? <p className={cn("ms-7 text-xs", todo.status === "failed" ? "text-destructive-foreground" : "text-warning-foreground")}>{todo.reason}</p>
    : null;
  return <li data-slot="agent-todo" data-status={todo.status} className="min-w-0 animate-in fade-in slide-in-from-top-1 duration-200 motion-reduce:animate-none">
    {todo.details ? <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="group/todo -mx-1.5 flex w-[calc(100%+0.75rem)] min-w-0 items-center gap-3 rounded-md px-1.5 py-1.5 text-left text-sm outline-none transition-colors hover:bg-muted/50 focus-visible:ring-2 focus-visible:ring-ring">
        {line}
        <ChevronDownIcon aria-hidden className="size-3.5 shrink-0 text-muted-foreground transition-transform duration-200 group-data-[panel-open]/todo:rotate-180 motion-reduce:transition-none" />
      </CollapsibleTrigger>
      {reason}
      {active && !open && todo.latest && <div className="ms-5 min-w-0">{todo.latest}</div>}
      <CollapsiblePanel><div className="ms-5 min-w-0 pb-1">{todo.details}</div></CollapsiblePanel>
    </Collapsible> : <>
      <div className="flex min-w-0 items-center gap-3 py-1.5 text-sm">{line}</div>
      {reason}
    </>}
  </li>;
}

/**
 * An agent's plan while it works. Fully controlled: pass the whole latest list each render; rows key off `id`. A long run of
 * finished steps at the top folds into one line so the current work stays in view.
 */
export function AgentTodos({ todos, explanation, running, title = "Tasks", foldAfter = 6, className }: {
  todos: readonly AgentTodoRow[];
  explanation?: string;
  /** The agent is working: the active mark turns and its timer ticks. */
  running: boolean;
  title?: string;
  foldAfter?: number;
  className?: string;
}) {
  const [open, setOpen] = useState(true);
  const [unfolded, setUnfolded] = useState(false);
  const now = useNow(running && todos.some((t) => t.status === "in_progress"));
  const counted = todos.filter((t) => t.status !== "cancelled");
  const done = counted.filter((t) => t.status === "completed").length;
  const total = counted.length;
  let folded = 0;
  if (!unfolded && todos.length > foldAfter) while (folded < todos.length && todos[folded]!.status === "completed") folded++;
  if (folded < 2) folded = 0;
  return <section data-slot="agent-todos" aria-label={title} className={cn("flex min-w-0 flex-col", className)}>
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="group/plan -mx-1.5 flex w-[calc(100%+0.75rem)] items-center gap-2 rounded-md px-1.5 py-1 text-left outline-none hover:bg-muted/40 focus-visible:ring-2 focus-visible:ring-ring">
        <span className="text-sm font-medium">{title}</span>
        <span className="text-sm text-muted-foreground tabular-nums">{done} of {total} done</span>
        <ChevronDownIcon aria-hidden className="ms-auto size-3.5 shrink-0 text-muted-foreground transition-transform duration-200 group-data-[panel-open]/plan:rotate-180 motion-reduce:transition-none" />
      </CollapsibleTrigger>
      <div role="progressbar" aria-label={`${title} progress`} aria-valuemin={0} aria-valuemax={total} aria-valuenow={done} className="mt-1.5 h-1 overflow-hidden rounded-full bg-muted">
        <div className="h-full origin-left rounded-full bg-foreground/70 transition-transform duration-300 ease-out motion-reduce:transition-none" style={{ transform: `scaleX(${total ? done / total : 0})` }} />
      </div>
      <CollapsiblePanel>
        {explanation && <p className="mt-2 text-sm text-muted-foreground">{explanation}</p>}
        <ol className="m-0 mt-1.5 flex list-none flex-col p-0">
          {folded > 0 && <li><button type="button" onClick={() => setUnfolded(true)} className="-mx-1.5 flex w-[calc(100%+0.75rem)] items-center gap-3 rounded-md px-1.5 py-1.5 text-left text-sm text-muted-foreground outline-none hover:bg-muted/50 hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring">
            <TodoMark status="completed" />{folded} completed steps
          </button></li>}
          {todos.slice(folded).map((todo) => <TodoRow key={todo.id} todo={todo} running={running} now={now} />)}
        </ol>
      </CollapsiblePanel>
    </Collapsible>
  </section>;
}
