/*
 * Orole's own implementation of assistant-ui's subagent list (https://www.assistant-ui.com/elements/subagent-list),
 * written from the published documentation (props, anatomy); nothing is copied. See docs/research/upstream-attribution.md.
 */
import { CheckIcon, CircleXIcon, LoaderCircleIcon, TriangleAlertIcon } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";
import { cn } from "@/lib/utils";
import { mono, paper } from "./surfaces";

export type SubagentStatus = "running" | "needs-you" | "done" | "failed";

export interface SubagentItem {
  name: string;
  /** The model or, in Orole, the harness badge drawn at the row's end. */
  model: ReactNode;
  status: SubagentStatus;
  /** A second line under the name: what the worker is doing right now. */
  activity?: string;
  /** Bar width and accessible value, `0…100`; absent while `running` = indeterminate shimmer. */
  progress?: number;
}

function StatusGlyph({ status }: { status: SubagentStatus }) {
  const base = "size-3.5 shrink-0";
  switch (status) {
    case "done": return <CheckIcon aria-hidden className={cn(base, "text-success")} />;
    case "failed": return <CircleXIcon aria-hidden className={cn(base, "text-destructive")} />;
    case "needs-you": return <TriangleAlertIcon aria-hidden className={cn(base, "text-warning")} />;
    default: return <LoaderCircleIcon aria-hidden className={cn(base, "animate-spin text-info motion-reduce:animate-none")} />;
  }
}

function Bar({ name, status, progress }: Pick<SubagentItem, "name" | "status" | "progress">) {
  const indeterminate = status === "running" && progress === undefined;
  const value = status === "done" ? 100 : progress === undefined || Number.isNaN(progress) ? 0 : Math.min(100, Math.max(0, progress));
  return <span role="progressbar" aria-label={`${name} progress`} aria-valuemin={0} aria-valuemax={100} aria-valuenow={indeterminate ? undefined : value} aria-busy={indeterminate || undefined}
    className="relative block h-0.5 w-full overflow-hidden rounded-full bg-muted">
    {indeterminate
      ? <span aria-hidden className="absolute inset-0 animate-skeleton [background:linear-gradient(90deg,transparent_30%,var(--color-info)_50%,transparent_70%)_0_0/300%_100%] motion-reduce:animate-none motion-reduce:bg-info/40 motion-reduce:[background-image:none]" />
      : <span aria-hidden className={cn("absolute inset-y-0 left-0 rounded-full transition-[width] duration-300 motion-reduce:transition-none", status === "failed" ? "bg-destructive" : status === "needs-you" ? "bg-warning" : "bg-success")} style={{ width: `${value}%` }} />}
  </span>;
}

/**
 * Parallel workers, one row each: a status glyph, the name, the model (or harness) slot, an optional activity line and a
 * progress bar; then an optional trailing summary card. Rows are buttons when `onOpen` is given; `selectedIndex` marks
 * the one whose work is open beside the conversation.
 */
export function SubagentList({ agents, summary, selectedIndex, onOpen, className, ...props }: Omit<ComponentProps<"ul">, "children"> & {
  agents: readonly SubagentItem[];
  /** A trailing card for the synthesis step, shimmering while its status is `running`. */
  summary?: SubagentItem;
  selectedIndex?: number;
  onOpen?: (index: number) => void;
}) {
  const row = (agent: SubagentItem, index: number | null) => {
    const selected = index !== null && index === selectedIndex;
    const body = <>
      <div className="flex min-w-0 items-center gap-2">
        <StatusGlyph status={agent.status} />
        <span className="min-w-0 flex-1 truncate text-sm leading-5">{agent.name}</span>
        {typeof agent.model === "string" ? <span className={cn(mono, "shrink-0")}>{agent.model}</span> : agent.model}
      </div>
      {agent.activity && <p className={cn("truncate pl-5.5 text-xs leading-4 text-muted-foreground", agent.status === "running" && "cv-shimmer")}>{agent.activity}</p>}
      <Bar name={agent.name} status={agent.status} progress={agent.progress} />
    </>;
    const cls = cn(paper, "flex w-full min-w-0 flex-col gap-1 px-2.5 py-1.5 text-start", selected && "border-ring/60 bg-accent");
    if (index === null || !onOpen) return <div data-slot="subagent-row" data-status={agent.status} className={cls}>{body}</div>;
    return <button type="button" data-slot="subagent-row" data-status={agent.status} aria-pressed={selected} onClick={() => onOpen(index)}
      className={cn(cls, "outline-none transition-colors hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring motion-reduce:transition-none")}>{body}</button>;
  };
  return <ul data-slot="subagent-list" className={cn("flex w-full min-w-0 flex-col gap-1.5", className)} {...props}>
    {agents.map((agent, i) => <li key={i} className="min-w-0">{row(agent, i)}</li>)}
    {summary && <li data-slot="subagent-summary" className="min-w-0">{row(summary, null)}</li>}
  </ul>;
}
