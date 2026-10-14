/*
 * Orole's own tool call rows. Their layout follows the public demo of the AI SDK Agents "Tool call card" component
 * (https://www.aisdkagents.com/ai-components/); none of that (paid) source was copied.
 */
import { CheckIcon, ChevronRightIcon, CircleAlertIcon, CircleDashedIcon, LoaderCircleIcon, ShieldAlertIcon, type LucideIcon } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";
import { Collapsible, CollapsiblePanel, CollapsibleTrigger } from "@/components/ui/collapsible";
import { cn } from "@/lib/utils";
import { surfaces } from "./surfaces";
import { Elapsed } from "./working-status";

/** `approval`: the call waits on a permission the user has to give. `unknown`: no result was ever recorded. */
export type ToolCallState = "running" | "ok" | "error" | "approval" | "unknown";

const BADGE: Record<ToolCallState, { icon: LucideIcon; tone: string; label: string }> = {
  ok: { icon: CheckIcon, tone: "bg-success/12 text-success", label: "Completed" },
  error: { icon: CircleAlertIcon, tone: "bg-destructive/12 text-destructive-foreground", label: "Failed" },
  approval: { icon: ShieldAlertIcon, tone: "bg-warning/15 text-warning-foreground", label: "Needs approval" },
  running: { icon: LoaderCircleIcon, tone: "bg-muted text-muted-foreground", label: "Running" },
  unknown: { icon: CircleDashedIcon, tone: "bg-muted text-muted-foreground", label: "No result recorded" },
};

/** The small rounded status square: a check, an alert, a shield for approval, a spinner while running. */
export function ToolCallBadge({ state, className }: { state: ToolCallState; className?: string }) {
  const { icon: Icon, tone } = BADGE[state];
  return <span aria-hidden className={cn("grid size-5 shrink-0 place-items-center rounded-md", tone, className)}>
    <Icon className={cn("size-3", state === "running" && "animate-spin motion-reduce:animate-none")} strokeWidth={2.25} />
  </span>;
}

/**
 * One tool call as a compact row: status badge, name, a one-line summary (the error in red when it failed, `Needs approval`
 * while a permission holds it), elapsed time and, with `details`, a chevron that opens the arguments and output. `children`
 * stay visible under the row, such as the agents the call started. Retry is deliberately absent: no harness offers it.
 */
export function ToolCallCard({ name, nameMono = false, summary, summaryMono = false, error, state, start, end, details, children, className, ...props }: Omit<ComponentProps<"div">, "children"> & {
  name: string;
  /** The name is a raw identifier (set in monospace) rather than words. */
  nameMono?: boolean;
  summary?: string;
  /** The summary is a command, path or pattern. */
  summaryMono?: boolean;
  /** A failed call's message. */
  error?: string;
  state: ToolCallState;
  start?: number | null;
  end?: number | null;
  /** Opened from the row: arguments and output. */
  details?: ReactNode;
  children?: ReactNode;
}) {
  const line = <>
    <ToolCallBadge state={state} />
    <span className="sr-only">{BADGE[state].label}: </span>
    <span className={cn("shrink-0 text-foreground", nameMono ? "font-mono text-xs" : "font-medium")}>{name}</span>
    {summary && <span className={cn("min-w-0 truncate text-muted-foreground", summaryMono && "font-mono text-xs")}>{summary}</span>}
    {state === "approval" && <span aria-hidden className="shrink-0 text-warning-foreground">Needs approval</span>}
    {error && <span className="min-w-0 truncate text-destructive-foreground">{error}</span>}
    <span className="min-w-2 flex-1" />
    <Elapsed start={start} end={end} live={state === "running" || state === "approval"} className="text-xs text-muted-foreground" />
  </>;
  const row = "flex w-full min-w-0 items-center gap-2 rounded-lg px-1.5 py-1.5 text-left text-[13px]";
  return <div data-slot="tool-call" data-state={state} className={cn("min-w-0", className)} {...props}>
    {details ? <Collapsible>
      <CollapsibleTrigger className={cn(row, "group/call outline-none transition-colors hover:bg-muted/50 focus-visible:ring-2 focus-visible:ring-ring data-[panel-open]:bg-muted/40 motion-reduce:transition-none")}>
        {line}
        <ChevronRightIcon aria-hidden className="size-3.5 shrink-0 text-muted-foreground transition-transform duration-150 group-data-[panel-open]/call:rotate-90 motion-reduce:transition-none" />
      </CollapsibleTrigger>
      <CollapsiblePanel><div className="mx-1.5 mb-1.5 mt-1 min-w-0">{details}</div></CollapsiblePanel>
    </Collapsible> : <div className={row}>{line}</div>}
    {children && <div className="ms-4 min-w-0">{children}</div>}
  </div>;
}

/**
 * Consecutive tool calls in one card. With a `title` the card opens with a summary row of the group: what is waiting or
 * running now (`Waiting for approval` + the call, ticking), or how many calls ran and how many failed.
 */
export function ToolCallGroup({ state, title, subject, start, end, children, className, ...props }: Omit<ComponentProps<"div">, "title"> & {
  state?: ToolCallState;
  title?: string;
  subject?: string;
  start?: number | null;
  end?: number | null;
}) {
  return <div data-slot="tool-call-group" className={cn(surfaces.paper, "min-w-0 overflow-hidden", className)} {...props}>
    {title && <div className="flex min-w-0 items-center gap-2 border-b px-2.5 py-2 text-[13px]">
      {state && <ToolCallBadge state={state} />}
      <span className="shrink-0 font-medium">{title}</span>
      {subject && <span className={cn("min-w-0 truncate", state === "error" ? "text-destructive-foreground" : "text-muted-foreground")}>{subject}</span>}
      <span className="min-w-2 flex-1" />
      <Elapsed start={start} end={end} live={state === "running" || state === "approval"} className="text-xs text-muted-foreground" />
    </div>}
    <div className="flex min-w-0 flex-col p-1">{children}</div>
  </div>;
}
