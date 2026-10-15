import { useState, type ReactNode } from "react";
import { ChevronDown, GripVertical } from "lucide-react";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Collapsible, CollapsiblePanel, CollapsibleTrigger } from "@/components/ui/collapsible";
import { cn } from "@/lib/utils";

const CLOSED_KEY = "orole.taskRail.closed";

/** Ids of the rail sections the person folded; remembered across tasks and restarts. */
function closedSections(): string[] {
  try {
    const value: unknown = JSON.parse(localStorage.getItem(CLOSED_KEY) ?? "[]");
    return Array.isArray(value) ? value.filter((id): id is string => typeof id === "string") : [];
  } catch { return []; }
}

/**
 * One card of the session rail: a title that folds it, a one-line `summary` shown while it is folded, and a grip at the top
 * right to drag it to another place in the rail (also by keyboard: focus the grip, Space, arrows, Space). Must render inside
 * the rail's `SortableContext`.
 */
export function RailSection({ id, title, summary, children }: { id: string; title: string; summary?: ReactNode; children: ReactNode }) {
  const [open, setOpen] = useState(() => !closedSections().includes(id));
  const { setNodeRef, setActivatorNodeRef, attributes, listeners, transform, transition, isDragging } = useSortable({ id });
  const toggle = (next: boolean) => {
    setOpen(next);
    try {
      const rest = closedSections().filter((closed) => closed !== id);
      localStorage.setItem(CLOSED_KEY, JSON.stringify(next ? rest : [...rest, id]));
    } catch {
      // The fold then lasts until the rail remounts.
    }
  };
  return <section ref={setNodeRef} aria-label={title} style={{ transform: CSS.Translate.toString(transform), transition }}
    className={cn("relative shrink-0 rounded-xl border bg-card text-card-foreground", isDragging && "z-10 shadow-lg")}>
    <Collapsible open={open} onOpenChange={toggle}>
      <div className="flex min-w-0 items-center pr-1.5">
        <CollapsibleTrigger className="flex h-9 min-w-0 flex-1 items-center gap-1 rounded-xl pl-3 text-left text-xs font-medium outline-none hover:bg-accent/40 focus-visible:ring-2 focus-visible:ring-ring">
          {title}
          <ChevronDown aria-hidden className={cn("size-3.5 shrink-0 text-muted-foreground transition-transform motion-reduce:transition-none", !open && "-rotate-90")} />
          {!open && summary && <span className="ml-auto min-w-0 truncate pl-2 font-normal text-muted-foreground tabular-nums">{summary}</span>}
        </CollapsibleTrigger>
        <button ref={setActivatorNodeRef} type="button" {...attributes} {...listeners} aria-label={`Move ${title}`}
          className={cn("ml-1 inline-flex size-6 shrink-0 touch-none items-center justify-center rounded-md text-muted-foreground/60 outline-none hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring", isDragging ? "cursor-grabbing" : "cursor-grab")}>
          <GripVertical aria-hidden className="size-3.5" />
        </button>
      </div>
      <CollapsiblePanel>
        <div className="flex flex-col gap-2 px-3 pb-3">{children}</div>
      </CollapsiblePanel>
    </Collapsible>
  </section>;
}
