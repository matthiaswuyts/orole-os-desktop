import { ChevronRightIcon, type LucideIcon } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";
import { Collapsible, CollapsiblePanel, CollapsibleTrigger } from "@/components/ui/collapsible";
import { cn } from "@/lib/utils";
import { surfaces } from "./surfaces";

export interface TimelineStep { id?: string; verb: string; chip: string; icon: LucideIcon }
export interface TimelineStat { file: string; added?: number; removed?: number }

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
          {rows.length > 0 && <ul aria-label="Steps" className="m-0 flex list-none flex-col gap-1.5 p-0">
            {rows.map((step, index) => {
              const Icon = step.icon;
              const last = streaming && index === rows.length - 1;
              return <li key={step.id ?? index} className="flex min-w-0 items-center gap-2 text-muted-foreground">
                <Icon aria-hidden className="size-3.5 shrink-0" />
                <span className={cn("shrink-0 font-medium", last && "cv-shimmer")}>{step.verb}</span>
                {step.chip && <span className={cn(surfaces.field, "min-w-0 truncate px-2 py-0.5")} title={step.chip}>{step.chip}</span>}
              </li>;
            })}
          </ul>}
          {stats.length > 0 && <ul aria-label="File changes" className="m-0 flex list-none flex-wrap gap-1.5 p-0">
            {stats.map((stat) => <li key={stat.file} className={cn(surfaces.field, "flex items-center gap-1.5 px-2 py-0.5")}>
              <span className="truncate">{stat.file}</span>
              {stat.added != null && <span className="text-success-foreground">+{stat.added}</span>}
              {stat.removed != null && <span className="text-destructive-foreground">-{stat.removed}</span>}
            </li>)}
          </ul>}
        </div>
      </CollapsiblePanel>
    </Collapsible>
  );
}
