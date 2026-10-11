import { ChevronRightIcon, CircleAlertIcon, CircleXIcon, LoaderCircleIcon, MessageSquareTextIcon, type LucideIcon } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";
import { Collapsible, CollapsiblePanel, CollapsibleTrigger } from "@/components/ui/collapsible";
import { cn } from "@/lib/utils";
import { TimelineSteps, type TimelineStep } from "./tool-timeline";

export type RunStatus = "running" | "requires-action" | "complete" | "cancelled" | "error" | "incomplete";

export interface RunEntry {
  /** Stable for the whole run: React keys and disclosure state hang off it. */
  id: string;
  kind: "commentary" | "tool";
  /** A short activity label: the commentary's title, or the tool step's verb. */
  label: string;
  /** Tool steps: the one thing worth showing next to the verb (a command, a file name). */
  chip?: string;
  icon?: LucideIcon;
  /** A failed tool shows an explicit error state. */
  failed?: boolean;
  /** An in-flight step shimmers. */
  running?: boolean;
  /** Hover text of the row, such as the time it happened. */
  title?: string;
  /** Opens from the row: commentary text, a tool's details. Rows without content are static. */
  content?: ReactNode;
  /** Shown under the row even while it is closed, such as a search excerpt. */
  aside?: ReactNode;
}

const statusIcon: Partial<Record<RunStatus, LucideIcon>> = { running: LoaderCircleIcon, error: CircleXIcon, cancelled: CircleAlertIcon, "requires-action": CircleAlertIcon };
const statusTone: Partial<Record<RunStatus, string>> = { error: "text-destructive-foreground", "requires-action": "text-warning-foreground" };

/**
 * assistant-ui's run activity (https://www.assistant-ui.com/elements/run-activity): one assistant run's public commentary and
 * tool history folded into a single disclosure, with a live status row ("Working", "Worked for 2m 13s"), the explicitly
 * identified final answer in `children`, and anything the user must act on in `attention`. Both stay visible when the
 * disclosure is collapsed. Props-driven: it executes nothing, classifies nothing and owns no runtime. The caller owns
 * `open` (initially false, keyed by run) and the application-supplied labels.
 *
 * Only `statusLabel` is announced while running, so clock ticks and streaming never interrupt a screen reader; once the run
 * settles the announcement carries the recorded `durationLabel` too. Missing timing stays absent.
 */
export function RunActivity({ status, statusLabel, durationLabel, open, onOpenChange, entries, attention, children, className, ...props }: Omit<ComponentProps<"div">, "children"> & {
  status: RunStatus;
  statusLabel: string;
  durationLabel?: ReactNode;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  entries: readonly RunEntry[];
  /** Approval requests, questions and errors: visible whether or not the run is expanded. */
  attention?: ReactNode;
  /** The final answer. */
  children?: ReactNode;
}) {
  const running = status === "running";
  const Icon = statusIcon[status];
  const steps: TimelineStep[] = entries.map((entry) => ({
    id: entry.id, verb: entry.label, chip: entry.chip ?? "", icon: entry.icon ?? MessageSquareTextIcon,
    failed: entry.failed, running: entry.running, title: entry.title, content: entry.content, aside: entry.aside,
  }));
  const summary = <>
    {Icon && <Icon aria-hidden className={cn("size-3.5 shrink-0", running && "animate-spin motion-reduce:animate-none")} />}
    <span className={cn("min-w-0 truncate font-medium", running && "cv-shimmer")}>{statusLabel}</span>
    {durationLabel != null && <span data-slot="run-activity-duration" className="shrink-0 tabular-nums">{durationLabel}</span>}
  </>;
  const tone = cn("flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground", statusTone[status]);
  return (
    <div data-slot="run-activity" data-status={status} aria-busy={running} className={cn("flex min-w-0 flex-col gap-2", className)} {...props}>
      {entries.length > 0 ? <Collapsible open={open} onOpenChange={onOpenChange} className="min-w-0">
        <CollapsibleTrigger className={cn(tone, "group/run -mx-1 max-w-full rounded-md px-1 py-0.5 text-left outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring")}>
          <ChevronRightIcon aria-hidden className="size-3.5 shrink-0 transition-transform group-data-[panel-open]/run:rotate-90 motion-reduce:transition-none" />
          {summary}
        </CollapsibleTrigger>
        <CollapsiblePanel>
          <div className="mt-1.5 flex min-w-0 flex-col gap-3 border-l ps-3">
            <TimelineSteps steps={steps} label="Activity" />
          </div>
        </CollapsiblePanel>
      </Collapsible> : <div className={tone}>{summary}</div>}
      <span role="status" className="sr-only">{statusLabel}{!running && durationLabel != null && <> {durationLabel}</>}</span>
      {attention}
      {children}
    </div>
  );
}
