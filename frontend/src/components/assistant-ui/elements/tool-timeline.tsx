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

/** One verb + icon + chip step of a timeline; with `content` the whole row opens into its detail. */
export function TimelineStepRow({ step, shimmer = false, className, ...props }: Omit<ComponentProps<"li">, "children"> & { step: TimelineStep; shimmer?: boolean }) {
  const Icon = step.failed ? CircleXIcon : step.icon;
  const verbLine = <>
    <Icon aria-hidden className={cn("size-3.5 shrink-0", step.failed && "text-destructive-foreground")} />
    <span className={cn("font-medium", step.chip ? "shrink-0" : "min-w-0 truncate", (shimmer || step.running) && "cv-shimmer", step.failed && "text-destructive-foreground")}>{step.verb}</span>
    {step.chip && <span className={cn(surfaces.field, "min-w-0 truncate px-2 py-0.5", step.failed && "border-destructive/40")} title={step.chip}>{step.chip}</span>}
    {step.failed && <span className="shrink-0 text-destructive-foreground">Failed</span>}
  </>;
  return <li data-slot="timeline-step" data-failed={step.failed || undefined} className={cn("min-w-0 text-muted-foreground", className)} {...props}>
    {step.content ? <Collapsible>
      <CollapsibleTrigger className="group/step flex w-full min-w-0 items-center gap-2 rounded-md text-left outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring" aria-label={`${step.verb}${step.chip ? ` ${step.chip}` : ""}${step.failed ? " (failed)" : ""}: show details`}>
        <ChevronRightIcon aria-hidden className="size-3 shrink-0 transition-transform group-data-[panel-open]/step:rotate-90 motion-reduce:transition-none" />
        {verbLine}
      </CollapsibleTrigger>
      <CollapsiblePanel><div className="mt-1.5 ms-5 min-w-0">{step.content}</div></CollapsiblePanel>
    </Collapsible> : <div className="flex min-w-0 items-center gap-2">{verbLine}</div>}
    {step.aside}
  </li>;
}

/** The vertical list of steps; with `streaming` the last verb shimmers. */
export function TimelineSteps({ steps, streaming = false, label = "Steps", className }: { steps: readonly TimelineStep[]; streaming?: boolean; label?: string; className?: string }) {
  if (steps.length === 0) return null;
  return <ul aria-label={label} className={cn("m-0 flex list-none flex-col gap-1.5 p-0", className)}>
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
