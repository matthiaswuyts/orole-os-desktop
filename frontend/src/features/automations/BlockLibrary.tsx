import { useState } from "react";
import { useDraggable } from "@dnd-kit/core";
import { Search } from "lucide-react";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import type { StepType } from "./api";
import { BlockIcon } from "./BlockIcon";

const itemClass = "flex w-full min-w-0 items-start gap-2 rounded-md px-2 py-1.5 text-left outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-60 disabled:hover:bg-transparent";

function ItemBody({ spec }: { spec: StepType }) {
  return <>
    <span className="mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-md border bg-muted/50"><BlockIcon type={spec.type} className="size-3.5" /></span>
    <span className="min-w-0 flex-1">
      <span className="block truncate text-sm font-medium">{spec.label}</span>
      <span className="block text-xs text-muted-foreground">{spec.available ? spec.description : spec.reason || "Not available on this PC."}</span>
    </span>
  </>;
}

function DragItem({ spec, onPick }: { spec: StepType; onPick: (type: string) => void }) {
  const { setNodeRef, attributes, listeners, isDragging } = useDraggable({ id: `new:${spec.type}`, data: { kind: "new", type: spec.type }, disabled: !spec.available });
  return <button ref={setNodeRef} type="button" {...attributes} {...listeners} disabled={!spec.available} aria-label={`${spec.label}: add to the end, or press Space to drag into the flow`}
    className={cn(itemClass, "touch-none", isDragging && "opacity-50")} onClick={() => onPick(spec.type)}><ItemBody spec={spec} /></button>;
}

/** Block types grouped as the daemon groups them, with search; unavailable types are listed with their reason. */
export function BlockPicker({ types, onPick, draggable = false, autoFocus = false }: { types: StepType[]; onPick: (type: string) => void; draggable?: boolean; autoFocus?: boolean }) {
  const [query, setQuery] = useState("");
  const q = query.trim().toLowerCase();
  const shown = types.filter((t) => !q || `${t.label} ${t.description} ${t.group} ${t.type}`.toLowerCase().includes(q));
  const groups = [...new Set(shown.map((t) => t.group || "Other"))];
  return <div className="flex min-h-0 flex-col gap-2">
    <div className="relative">
      <Search aria-hidden className="pointer-events-none absolute top-1/2 left-2.5 z-10 size-3.5 -translate-y-1/2 text-muted-foreground" />
      <Input size="sm" autoFocus={autoFocus} aria-label="Search blocks" placeholder="Search blocks" className="[&_input]:ps-7" value={query} onChange={(e) => setQuery(e.target.value)} />
    </div>
    {!shown.length && <p className="px-2 py-3 text-xs text-muted-foreground">{types.length ? "No block matches." : "The daemon has not listed any blocks."}</p>}
    {groups.map((group) => <section key={group} aria-label={group} className="flex flex-col">
      <h3 className="px-2 pt-1 pb-0.5 text-[11px] font-medium text-muted-foreground uppercase tracking-wide">{group}</h3>
      <ul className="flex flex-col">{shown.filter((t) => (t.group || "Other") === group).map((spec) => <li key={spec.type}>
        {draggable ? <DragItem spec={spec} onPick={onPick} /> : <button type="button" disabled={!spec.available} className={itemClass} onClick={() => onPick(spec.type)}><ItemBody spec={spec} /></button>}
      </li>)}</ul>
    </section>)}
  </div>;
}

export function BlockLibrary({ types, onPick, problem }: { types: StepType[]; onPick: (type: string) => void; problem: string | null }) {
  return <section aria-label="Block library" className="flex flex-col gap-2 p-3">
    <header>
      <h2 className="text-sm font-medium">Blocks</h2>
      <p className="text-xs text-muted-foreground">{problem ?? "Drag a block into the flow, or click to add it at the end."}</p>
    </header>
    <BlockPicker types={types} onPick={onPick} draggable={!problem} />
  </section>;
}
