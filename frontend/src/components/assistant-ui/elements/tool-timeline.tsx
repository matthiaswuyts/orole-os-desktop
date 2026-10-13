import { ChevronRightIcon, CircleXIcon, type LucideIcon } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";
import { Collapsible, CollapsiblePanel, CollapsibleTrigger } from "@/components/ui/collapsible";
import { cn } from "@/lib/utils";
import { surfaces } from "./surfaces";

export interface TimelineStep {
  id?: string;
  verb: string;
  chip: string;
  icon: LucideIcon;
  /** The chip is a command, path or pattern: it is set in monospace. */
  code?: boolean;
  /** The step's call failed: an explicit error state next to the chip, never only a colour. */
  failed?: boolean;
  /** The step is still in flight: its verb shimmers. */
  running?: boolean;
  /** Detail revealed by the row's own disclosure (input, output, a longer note). Rows without content are static. */
  content?: ReactNode;
  /** Shown under the row whether or not it is open, such as a search excerpt. */
  aside?: ReactNode;
}
export interface TimelineStat { file: string; added?: number; removed?: number }

const rowClass = "flex w-full min-w-0 items-center gap-2 rounded-md px-1.5 py-1 text-left";

/** One icon + verb + detail step of a timeline; with `content` the whole row opens into its detail. */
export function TimelineStepRow({ step, shimmer = false, className, ...props }: Omit<ComponentProps<"li">, "children"> & { step: TimelineStep; shimmer?: boolean }) {
  const Icon = step.failed ? CircleXIcon : step.icon;
  const verbLine = <>
    <Icon aria-hidden className={cn("size-3.5 shrink-0 text-muted-foreground/70", step.failed && "text-destructive-foreground")} />
    <span className={cn("text-foreground/85", step.chip ? "shrink-0" : "min-w-0 truncate", (shimmer || step.running) && "cv-shimmer", step.failed && "text-destructive-foreground")}>{step.verb}</span>
    {step.chip && <span className={cn("min-w-0 truncate text-muted-foreground", step.code && "font-mono text-[11px]")}>{step.chip}</span>}
    {step.failed && <span className="shrink-0 text-destructive-foreground">Failed</span>}
  </>;
  return <li data-slot="timeline-step" data-failed={step.failed || undefined} className={cn("min-w-0 text-xs text-muted-foreground", className)} {...props}>
    {step.content ? <Collapsible>
      <CollapsibleTrigger className={cn(rowClass, "group/step outline-none transition-colors hover:bg-muted/60 focus-visible:ring-2 focus-visible:ring-ring data-[panel-open]:bg-muted/40")} aria-label={`${step.verb}${step.chip ? ` ${step.chip}` : ""}${step.failed ? " (failed)" : ""}: show details`}>
        {verbLine}
        <ChevronRightIcon aria-hidden className="ms-auto size-3.5 shrink-0 opacity-0 transition-[transform,opacity] duration-150 group-hover/step:opacity-100 group-focus-visible/step:opacity-100 group-data-[panel-open]/step:rotate-90 group-data-[panel-open]/step:opacity-100 motion-reduce:transition-none" />
      </CollapsibleTrigger>
      <CollapsiblePanel><div className="mb-1.5 ms-[1.375rem] mt-1 min-w-0">{step.content}</div></CollapsiblePanel>
    </Collapsible> : <div className={rowClass}>{verbLine}</div>}
    {step.aside}
  </li>;
}

/** The vertical list of steps; with `streaming` the last verb shimmers. */
export function TimelineSteps({ steps, streaming = false, label = "Steps", className }: { steps: readonly TimelineStep[]; streaming?: boolean; label?: string; className?: string }) {
  if (steps.length === 0) return null;
  return <ul aria-label={label} className={cn("m-0 flex list-none flex-col p-0", className)}>
    {steps.map((step, index) => <TimelineStepRow key={step.id ?? index} step={step} shimmer={streaming && index === steps.length - 1} />)}
  </ul>;
}

/** File chips with +/- line counts. */
export function TimelineStats({ stats }: { stats: readonly TimelineStat[] }) {
  if (stats.length === 0) return null;
  return <ul aria-label="File changes" className="m-0 flex list-none flex-wrap gap-1.5 p-0">
    {stats.map((stat) => <li key={stat.file} className={cn(surfaces.field, "flex items-center gap-1.5 px-2 py-0.5")}>
      <span className="truncate">{stat.file}</span>
      {stat.added != null && <span className="text-success-foreground">+{stat.added}</span>}
      {stat.removed != null && <span className="text-destructive-foreground">-{stat.removed}</span>}
    </li>)}
  </ul>;
}

/**
 * assistant-ui's tool timeline (https://www.assistant-ui.com/elements/tool-timeline): everything one assistant turn did,
 * as a single collapsed line that opens into a vertical trace of verb, icon and chip per step. `children` render inside
 * the open panel above the steps, for richer detail such as a web search or a research outline.
 */
export function ToolTimeline({ steps, visibleSteps, streaming, open, onOpenChange, restingLabel, activeLabel, stats, className, children, ...props }: Omit<ComponentProps<"div">, "children"> & {
  steps: readonly TimelineStep[];
  visibleSteps: number;
  streaming: boolean;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  restingLabel: string;
  activeLabel: string;
  stats: readonly TimelineStat[];
  children?: ReactNode;
}) {
  const shown = Math.max(0, Math.min(steps.length, Number.isFinite(visibleSteps) ? Math.floor(visibleSteps) : 0));
  const rows = steps.slice(0, shown);
  return (
    <Collapsible open={open} onOpenChange={onOpenChange} render={<div data-slot="tool-timeline" className={cn("min-w-0 text-xs", className)} {...props} />}>
      <CollapsibleTrigger className="group/timeline flex items-center gap-1 rounded-md py-0.5 text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring">
        <ChevronRightIcon aria-hidden className="size-3.5 transition-transform group-data-[panel-open]/timeline:rotate-90" />
        <span className={cn("font-medium", streaming && "cv-shimmer")}>{streaming ? activeLabel : restingLabel}</span>
      </CollapsibleTrigger>
      <CollapsiblePanel>
        <div className="mt-1.5 flex min-w-0 flex-col gap-3 border-l ps-3">
          {children}
          <TimelineSteps steps={rows} streaming={streaming} />
          <TimelineStats stats={stats} />
        </div>
      </CollapsiblePanel>
    </Collapsible>
  );
}
