import { CheckIcon, ChevronRightIcon, CircleIcon, CircleXIcon, LoaderCircleIcon, TriangleAlertIcon } from "lucide-react";
import type { AgentsStatus, MsgPart } from "@/mygo";
import { ProviderLogo } from "@/features/agents/ProviderLogo";
import { kindLabel, sessionTitle } from "@/features/agents/format";
import { cn } from "@/lib/utils";
import { useChat } from "@/stores/chat";
import { TASK_STATUS_LABEL, useTaskSession } from "../taskSession";

/** Spinner while the agent works, a check when its turn is done, a warning when it needs you, a cross when it failed. */
export function TaskStatusGlyph({ status, className }: { /** absent while the task is still loading */ status?: AgentsStatus; className?: string }) {
  const base = cn("size-3.5 shrink-0", className);
  switch (status) {
    case undefined:
    case "working":
      return <LoaderCircleIcon aria-hidden className={cn(base, "animate-spin motion-reduce:animate-none", status ? "text-info" : "text-muted-foreground")} />;
    case "done":
      return <CheckIcon aria-hidden className={cn(base, "text-success")} />;
    case "needs-you":
      return <TriangleAlertIcon aria-hidden className={cn(base, "text-warning")} />;
    case "failed":
      return <CircleXIcon aria-hidden className={cn(base, "text-destructive")} />;
    default:
      return <CircleIcon aria-hidden className={cn(base, "text-muted-foreground")} />;
  }
}

/** The coding agent a task runs on, as a small logo-and-name badge. */
export function HarnessBadge({ kind }: { kind: string }) {
  return <span data-slot="harness-badge" className="inline-flex shrink-0 items-center gap-1 rounded-md bg-muted px-1.5 py-0.5 text-[11px] leading-none text-muted-foreground [&>span]:size-3 [&>svg]:size-3">
    <ProviderLogo kind={kind} />{kindLabel(kind)}
  </span>;
}

/** One code task a bot started: its live status, title and harness. Opens the task beside the conversation. */
export function TaskChip({ part }: { part: MsgPart }) {
  const id = part.id ?? "";
  const { session, missing } = useTaskSession(id);
  const open = useChat((s) => s.taskPaneId === id);
  const title = session ? sessionTitle(session) : part.title || "Code task";
  const kind = session?.kind || part.name || "";
  const status = session?.status;
  const statusText = missing ? "Unavailable" : status ? TASK_STATUS_LABEL[status] : "Loading";
  return <button type="button" data-slot="task-chip" data-status={missing ? "missing" : status} aria-pressed={open}
    onClick={() => useChat.getState().openTaskPane(id)}
    className={cn("group flex w-full max-w-md min-w-0 items-center gap-2 rounded-lg border bg-card/50 px-2.5 py-1.5 text-start text-sm outline-none transition-colors hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring motion-reduce:transition-none",
      open && "border-ring/60 bg-accent")}>
    {missing ? <CircleIcon aria-hidden className="size-3.5 shrink-0 text-muted-foreground" /> : <TaskStatusGlyph status={status} />}
    <span className="sr-only">{statusText}:</span>
    <span className="min-w-0 flex-1 truncate" title={title}>{title}</span>
    {kind && <HarnessBadge kind={kind} />}
    <ChevronRightIcon aria-hidden className="size-3.5 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5 motion-reduce:transition-none" />
  </button>;
}

/** The tasks one reply started, one chip each. */
export function TaskChips({ parts }: { parts: readonly MsgPart[] }) {
  return <ul data-slot="task-chips" aria-label="Code tasks" className="flex w-full flex-col gap-1.5">
    {parts.map((part, i) => <li key={part.id || i} className="min-w-0"><TaskChip part={part} /></li>)}
  </ul>;
}
