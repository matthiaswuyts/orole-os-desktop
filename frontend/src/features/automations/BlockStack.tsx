import { useState, type ReactNode } from "react";
import { useDroppable } from "@dnd-kit/core";
import { SortableContext, useSortable, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { GripVertical, Plus, Trash2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Popover, PopoverPopup, PopoverTrigger } from "@/components/ui/popover";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import type { Step } from "./api";
import { BlockPicker } from "./BlockLibrary";
import { LANES, LANE_LABEL, blockSummary, insertProblem, issueCount, laneKey } from "./flow";
import { BlockIcon } from "./BlockIcon";
import { useFlow, type DragData } from "./flowContext";

function AddBlock({ lane, index, label, className, children }: { lane: string; index: number; label: string; className: string; children: ReactNode }) {
  const { tree, types, insert } = useFlow();
  const [open, setOpen] = useState(false);
  const problem = insertProblem(tree, lane);
  const trigger = <PopoverTrigger disabled={!!problem} aria-label={label}
    render={<button type="button" className={cn("outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50", className)} />}>{children}</PopoverTrigger>;
  return <Popover open={open} onOpenChange={setOpen}>
    {problem ? <Tooltip><TooltipTrigger render={<span className="inline-flex" />}>{trigger}</TooltipTrigger><TooltipPopup className="max-w-xs">{problem}</TooltipPopup></Tooltip> : trigger}
    <PopoverPopup className="w-80" side="right" align="start">
      <div className="max-h-[min(28rem,70vh)] overflow-auto">
        <BlockPicker types={types} autoFocus onPick={(type) => { insert(lane, index, type); setOpen(false); }} />
      </div>
    </PopoverPopup>
  </Popover>;
}

/** The thin connector between blocks with a + to insert there. */
function InsertPoint({ lane, index, before }: { lane: string; index: number; before: string }) {
  return <div className="relative flex h-5 items-center justify-center">
    <span aria-hidden className="absolute inset-y-0 left-1/2 w-px bg-border" />
    <AddBlock lane={lane} index={index} label={`Add a block before ${before}`}
      className="relative flex size-4 items-center justify-center rounded-full border bg-background text-muted-foreground opacity-70 transition-opacity hover:text-foreground hover:opacity-100 focus-visible:opacity-100"><Plus aria-hidden className="size-3" /></AddBlock>
  </div>;
}

/** End of a lane: drop target and "Add block". */
function LaneEnd({ lane, count, where }: { lane: string; count: number; where: string }) {
  const { tree } = useFlow();
  const problem = insertProblem(tree, lane);
  const { setNodeRef, isOver } = useDroppable({ id: `lane:${lane}`, data: { kind: "lane", lane } satisfies DragData, disabled: !!problem });
  return <div ref={setNodeRef} className={cn("rounded-lg border border-transparent transition-colors", !count && "border-dashed border-border p-2", isOver && "border-primary bg-primary/8")}>
    {!count && <p className="mb-1 text-center text-xs text-muted-foreground">{problem ?? "Drop a block here"}</p>}
    <div className="flex justify-center">
      <AddBlock lane={lane} index={count} label={`Add a block at the end of ${where}`} className="inline-flex h-6 items-center gap-1 rounded-md px-2 text-xs text-muted-foreground hover:bg-accent hover:text-foreground">
        <Plus aria-hidden className="size-3.5" />Add block
      </AddBlock>
    </div>
  </div>;
}

/** One lane of blocks (the flow itself, or a condition's "If true"/"Otherwise"), sortable by drag. */
export function BlockStack({ steps, lane, depth, where }: { steps: Step[]; lane: string; depth: number; where: string }) {
  return <SortableContext id={lane} items={steps.map((s) => s.id)} strategy={verticalListSortingStrategy}>
    <ol aria-label={where} className="flex flex-col">
      {steps.map((step, index) => <li key={step.id} className="flex flex-col">
        <InsertPoint lane={lane} index={index} before={step.name || step.id} />
        <BlockCard step={step} lane={lane} depth={depth} />
      </li>)}
      <li className={cn(steps.length > 0 && "pt-1")}>
        {steps.length > 0 && <div aria-hidden className="mx-auto h-3 w-px bg-border" />}
        <LaneEnd lane={lane} count={steps.length} where={where} />
      </li>
    </ol>
  </SortableContext>;
}

function BlockCard({ step, lane, depth }: { step: Step; lane: string; depth: number }) {
  const { types, issues, names, selected, select, remove } = useFlow();
  const spec = types.find((t) => t.type === step.type);
  const { setNodeRef, setActivatorNodeRef, attributes, listeners, transform, transition, isDragging, isOver, active } = useSortable({ id: step.id, data: { kind: "block", lane } satisfies DragData });
  const drag = active?.data.current as DragData | undefined;
  const dropBefore = isOver && !!drag && (drag.kind === "new" || drag.lane !== lane);
  const count = issueCount(issues, step.id);
  const label = spec?.label ?? step.type;
  const summary = blockSummary(step, names);
  const current = selected === step.id;
  return <div ref={setNodeRef} style={{ transform: CSS.Translate.toString(transform), transition }}
    className={cn("relative rounded-lg border bg-card shadow-xs/5", current && "border-primary ring-1 ring-primary/40", count > 0 && !current && "border-destructive/50", isDragging && "opacity-40")}>
    {dropBefore && <span aria-hidden className="absolute inset-x-2 -top-3 h-0.5 rounded-full bg-primary" />}
    <div className="flex items-center gap-1.5 py-1.5 pr-1.5 pl-1">
      <button ref={setActivatorNodeRef} type="button" {...attributes} {...listeners} aria-label={`Move ${step.name || label} (${step.id})`}
        className="flex size-6 shrink-0 cursor-grab touch-none items-center justify-center rounded-md text-muted-foreground outline-none hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring active:cursor-grabbing">
        <GripVertical className="size-3.5" />
      </button>
      <span className="flex size-6 shrink-0 items-center justify-center rounded-md border bg-muted/50"><BlockIcon type={step.type} className="size-3.5" /></span>
      <button type="button" aria-pressed={current} onClick={() => select(step.id)} className="min-w-0 flex-1 rounded-md px-1 py-0.5 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring">
        <span className="flex min-w-0 items-baseline gap-1.5"><span className="truncate text-sm font-medium">{step.name || label}</span><span className="shrink-0 font-mono text-[11px] text-muted-foreground">{step.id}</span></span>
        <span className="block truncate text-xs text-muted-foreground">{summary || "Not set up yet"}</span>
      </button>
      {count > 0 && <Badge size="sm" variant="error">{count === 1 ? "1 issue" : `${count} issues`}</Badge>}
      {spec && !spec.available && (spec.reason ? <Tooltip><TooltipTrigger render={<Badge size="sm" variant="warning" />}>Unavailable</TooltipTrigger><TooltipPopup className="max-w-xs">{spec.reason}</TooltipPopup></Tooltip> : <Badge size="sm" variant="warning">Unavailable</Badge>)}
      {!spec && <Badge size="sm" variant="warning">Unknown block</Badge>}
      <Button size="icon-xs" variant="ghost" aria-label={`Remove ${step.name || label} (${step.id})`} onClick={() => remove(step.id)}><Trash2 /></Button>
    </div>
    {step.type === "condition" && <div className="flex flex-col gap-2 border-t px-2 pt-2 pb-2">
      {LANES.map((l) => <div key={l} className="flex flex-col gap-1">
        <p className="text-[11px] font-medium text-muted-foreground">{LANE_LABEL[l]}</p>
        <div className="ml-1 border-l-2 pl-3">
          <BlockStack steps={step[l] ?? []} lane={laneKey(step.id, l)} depth={depth + 1} where={`the ${LANE_LABEL[l]} lane of ${step.id}`} />
        </div>
      </div>)}
    </div>}
  </div>;
}
