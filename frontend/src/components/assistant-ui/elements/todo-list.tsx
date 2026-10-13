/*
 * Orole's own implementation of assistant-ui's todo list (https://www.assistant-ui.com/elements/todo-list), written from
 * the published documentation (props, anatomy, counting rules); nothing is copied. See docs/research/upstream-attribution.md.
 */
import { CheckSquareIcon, LoaderCircleIcon, MinusIcon, SquareIcon, XIcon } from "lucide-react";
import { useRef, useState, type ComponentProps } from "react";
import { cn } from "@/lib/utils";
import { mono } from "./surfaces";

export type TodoStatus = "pending" | "active" | "done" | "failed" | "cancelled";

export interface TodoItem {
  /** Row key; a stable id keeps the entrance animation from replaying on every rewrite. */
  id: string;
  text: string;
  status: TodoStatus;
  /** Detail under a pending, active or done item. */
  description?: string;
  /** Why a failed item failed; takes the description's place. */
  reason?: string;
}

const STATUS_LABEL: Record<TodoStatus, string> = { pending: "Pending", active: "In progress", done: "Done", failed: "Failed", cancelled: "Cancelled" };

/**
 * The counter's numerator and denominator: `done` counts toward both, `failed` only toward the denominator (settled but
 * not a success), `cancelled` toward neither (it left the plan).
 */
export function todoProgress(items: readonly Pick<TodoItem, "status">[]): { done: number; total: number } {
  let done = 0;
  let total = 0;
  for (const item of items) {
    if (item.status === "cancelled") continue;
    total++;
    if (item.status === "done") done++;
  }
  return { done, total };
}

/** The rows a folded list shows: the first `maxVisible`, plus any later active or failed row. */
export function visibleTodos<T extends Pick<TodoItem, "status">>(items: readonly T[], maxVisible: number | undefined): readonly T[] {
  if (maxVisible === undefined) return items;
  const keep = Math.min(items.length, Math.max(0, Math.floor(maxVisible)));
  return items.filter((item, i) => i < keep || item.status === "active" || item.status === "failed");
}

function StatusIcon({ status }: { status: TodoStatus }) {
  const base = "size-3.5 shrink-0";
  switch (status) {
    case "done": return <CheckSquareIcon aria-hidden className={cn(base, "text-success")} />;
    case "active": return <LoaderCircleIcon aria-hidden className={cn(base, "animate-spin text-info motion-reduce:animate-none")} />;
    case "failed": return <XIcon aria-hidden className={cn(base, "text-destructive")} />;
    case "cancelled": return <MinusIcon aria-hidden className={cn(base, "text-muted-foreground")} />;
    default: return <SquareIcon aria-hidden className={cn(base, "text-muted-foreground")} />;
  }
}

/**
 * The agent's own working list, rewritten as it discovers what else is needed. Fully controlled: the caller passes the
 * whole list each render; rows key off `id`, so a rewrite with the same ids restyles rows in place while a new id fades
 * and slides in.
 */
export function TodoList({ items, title = "Todos", description, maxVisible, revision, className, ...props }: Omit<ComponentProps<"div">, "children" | "title"> & {
  items: readonly TodoItem[];
  title?: string;
  description?: string;
  /** Rows shown before folding; later active and failed rows stay visible. */
  maxVisible?: number;
  /** Appends `· rev {revision}` to the counter. */
  revision?: number;
}) {
  const [expanded, setExpanded] = useState(false);
  // Ids present at first render never animate in; an id that arrives with a later list does, and keeps its entrance class
  // for as long as that list is the one rendered, so an unrelated re-render never cuts the animation short.
  const memo = useRef<{ items: readonly TodoItem[]; seen: Set<string>; fresh: Set<string> } | null>(null);
  if (memo.current === null) memo.current = { items, seen: new Set(items.map((item) => item.id)), fresh: new Set() };
  else if (memo.current.items !== items) {
    const fresh = new Set<string>();
    for (const item of items) if (!memo.current.seen.has(item.id)) { fresh.add(item.id); memo.current.seen.add(item.id); }
    memo.current = { items, seen: memo.current.seen, fresh };
  }
  const fresh = memo.current.fresh;
  const { done, total } = todoProgress(items);
  const shown = expanded ? items : visibleTodos(items, maxVisible);
  const hidden = items.length - shown.length;
  const rows = shown.map((item) => {
    const detail = item.status === "failed" ? item.reason : item.status === "cancelled" ? undefined : item.description;
    const struck = item.status === "done" || item.status === "cancelled";
    return <li key={item.id} data-status={item.status} className={cn("flex min-w-0 items-start gap-2 py-0.5", fresh.has(item.id) && "animate-in fade-in slide-in-from-top-1 duration-200 motion-reduce:animate-none")}>
      <span className="flex h-5 items-center"><StatusIcon status={item.status} /></span>
      <span className="sr-only">{STATUS_LABEL[item.status]}:</span>
      <div className="flex min-w-0 flex-1 flex-col">
        <span className={cn("text-sm leading-5", struck && "text-muted-foreground line-through", item.status === "active" && "font-medium")}>{item.text}</span>
        {detail && <p className={cn("text-xs leading-4", item.status === "failed" ? "text-destructive" : "text-muted-foreground")}>{detail}</p>}
      </div>
    </li>;
  });
  return <div data-slot="todo-list" className={cn("flex w-full min-w-0 flex-col gap-1.5", className)} {...props}>
    <div className="flex items-start justify-between gap-3">
      <div className="min-w-0">
        <span className="text-[13px] font-medium">{title}</span>
        {description && <p className="text-xs text-muted-foreground">{description}</p>}
      </div>
      <span className={cn(mono, "shrink-0")}>{done}/{total}{revision !== undefined && ` · rev ${revision}`}</span>
    </div>
    <ul aria-label={title} className="flex flex-col">{rows}</ul>
    {hidden > 0 && <button type="button" aria-expanded={false} onClick={() => setExpanded(true)}
      className="self-start rounded-md px-1 text-xs text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring">
      Show {hidden} more
    </button>}
  </div>;
}
