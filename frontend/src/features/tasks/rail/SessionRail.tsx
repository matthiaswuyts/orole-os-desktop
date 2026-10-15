import { useState, type ReactNode } from "react";
import { FolderIcon, GitBranchIcon, MonitorIcon, type LucideIcon } from "lucide-react";
import { closestCenter, DndContext, KeyboardSensor, PointerSensor, useSensor, useSensors, type DragEndEvent, type Modifier } from "@dnd-kit/core";
import { arrayMove, SortableContext, sortableKeyboardCoordinates, verticalListSortingStrategy } from "@dnd-kit/sortable";
import type { Event as AgentEvent, Session } from "@/mygo";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { folderOf } from "@/features/agents/format";
import { useBoxes } from "@/stores/boxes";
import { useProjects } from "@/stores/projects";
import { ContextWindow, contextUsage } from "../ComposerContext";
import { BackgroundTasksSection } from "./BackgroundTasksSection";
import { ChangesSection } from "./ChangesSection";
import { ConnectorsSection } from "./ConnectorsSection";
import { RailSection } from "./RailSection";

/** The rail's width; the conversation keeps its overlays clear of it. */
export const RAIL_WIDTH = "18rem";

const SECTIONS = ["environment", "context", "connectors", "changes", "background"] as const;
type SectionId = (typeof SECTIONS)[number];
const ORDER_KEY = "orole.taskRail.order";

/** Sections only move up and down the rail, never sideways. */
const verticalOnly: Modifier = ({ transform }) => ({ ...transform, x: 0 });

/** The order the person dragged the sections into; sections it does not name (new ones) follow in their default order. */
function savedOrder(): SectionId[] {
  let saved: unknown = [];
  try { saved = JSON.parse(localStorage.getItem(ORDER_KEY) ?? "[]"); } catch { /* default order */ }
  const known = Array.isArray(saved) ? saved.filter((id): id is SectionId => SECTIONS.includes(id as SectionId)) : [];
  const unique = [...new Set(known)];
  return [...unique, ...SECTIONS.filter((id) => !unique.includes(id))];
}

/**
 * The session rail beside an open task: where it runs, what its context holds, the connectors its agent reaches through the
 * Orole gateway, its changes with Create PR, and the background agents it started. `events` are the task's loaded history.
 * Sections are reordered by their grips; the order is remembered.
 */
export function SessionRail({ session, events }: { session: Session; events: readonly AgentEvent[] }) {
  const [order, setOrder] = useState(savedOrder);
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );
  const onDragEnd = ({ active, over }: DragEndEvent) => {
    if (!over || active.id === over.id) return;
    const next = arrayMove(order, order.indexOf(active.id as SectionId), order.indexOf(over.id as SectionId));
    setOrder(next);
    try { localStorage.setItem(ORDER_KEY, JSON.stringify(next)); } catch { /* the order then lasts until the rail remounts */ }
  };
  const { fraction } = contextUsage(session.usage);
  const section = (id: SectionId) => {
    switch (id) {
      case "environment": return <EnvironmentSection key={id} session={session} />;
      case "context": return <RailSection key={id} id={id} title="Context" summary={fraction === null ? undefined : `${Math.round(fraction * 100)}% used`}>
        <ContextWindow usage={session.usage} />
      </RailSection>;
      case "connectors": return <ConnectorsSection key={id} />;
      case "changes": return <ChangesSection key={id} session={session} />;
      case "background": return <BackgroundTasksSection key={id} events={events} />;
    }
  };
  return <aside aria-label="Session rail" className="flex h-full w-full flex-col gap-2 overflow-y-auto overscroll-contain px-1 py-2">
    <DndContext sensors={sensors} collisionDetection={closestCenter} modifiers={[verticalOnly]} onDragEnd={onDragEnd}>
      <SortableContext items={order} strategy={verticalListSortingStrategy}>{order.map(section)}</SortableContext>
    </DndContext>
  </aside>;
}

function EnvironmentSection({ session }: { session: Session }) {
  const machineId = session.machine || "local";
  const machine = useBoxes((state) => state.machines.find((item) => item.id === machineId)?.name) ?? (machineId === "local" ? "This computer" : machineId);
  const project = useProjects((state) => session.projectId ? state.projects.find((item) => item.id === session.projectId)?.name : undefined) ?? folderOf(session);
  return <RailSection id="environment" title="Environment" summary={session.branch || project}>
    <ul className="flex flex-col gap-0.5">
      <EnvironmentRow icon={MonitorIcon} label={machine} />
      <EnvironmentRow icon={FolderIcon} label={project} detail={session.dir ? <Tooltip>
        <TooltipTrigger render={<span tabIndex={0} className="min-w-0 flex-1 truncate text-xs text-muted-foreground" />}>{session.dir}</TooltipTrigger>
        <TooltipPopup className="max-w-sm break-all">{session.dir}</TooltipPopup>
      </Tooltip> : null} />
      <EnvironmentRow icon={GitBranchIcon} label={session.branch || "No branch"} detail={session.worktreeOwned ? <span className="shrink-0 text-xs text-muted-foreground">worktree</span> : null} />
    </ul>
  </RailSection>;
}

function EnvironmentRow({ icon: Icon, label, detail }: { icon: LucideIcon; label: string; detail?: ReactNode }) {
  return <li className="flex min-h-7 min-w-0 items-center gap-2 text-[13px]">
    <Icon aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
    <span className="min-w-0 truncate">{label}</span>
    {detail && <span aria-hidden className="shrink-0 text-muted-foreground">·</span>}
    {detail}
  </li>;
}
