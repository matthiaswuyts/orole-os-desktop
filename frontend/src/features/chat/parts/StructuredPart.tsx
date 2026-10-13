import { Component, useState, type ErrorInfo, type ReactNode } from "react";
import { Chart } from "@/components/assistant-ui/elements/chart";
import { ComparisonCard, type ComparisonOption } from "@/components/assistant-ui/elements/comparison-card";
import { DataTable, type DataTableColumn, type DataTableRow } from "@/components/assistant-ui/elements/data-table";
import { Diagram } from "@/components/assistant-ui/elements/diagram";
import { FlowGraph, type FlowEdge, type FlowNode } from "@/components/assistant-ui/elements/flow-graph";
import { JobProgress, type JobStage } from "@/components/assistant-ui/elements/job-progress";
import { MathBlock, type MathStep } from "@/components/assistant-ui/elements/math-block";
import { SpecSheet, type SpecRow } from "@/components/assistant-ui/elements/spec-sheet";
import { Timeline, type TimelineEvent } from "@/components/assistant-ui/elements/timeline";
import { TodoList, type TodoItem } from "@/components/assistant-ui/elements/todo-list";
import type { MsgPart } from "@/mygo";
import { useChat } from "@/stores/chat";
import { AutomationProposalCard, type AutomationProposalSpec } from "./AutomationProposalCard";
import { DocumentCard, type DocumentSpec } from "./DocumentCard";
import { SkillProposalCard, type SkillProposalSpec } from "./SkillProposalCard";

/**
 * The component kinds the daemon's show_* tools emit (internal/chat/tools/show.go) plus the proposal cards: `skill` (propose_skill) and
 * `automation` (propose_automation), the agent's working list (`todo`, update_todos) and a written document (`document`, write_document).
 */
export const UI_KINDS = ["table", "chart", "diagram", "flow", "math", "spec", "comparison", "timeline", "job", "skill", "automation", "todo", "document"] as const;
export type UIKind = (typeof UI_KINDS)[number];

/** Where a part sits in its conversation, so a document card can open that part beside the thread. */
export interface PartAt { messageId: string; partIndex: number }

interface DiagramNode { id: string; label: string; x: number; y: number; width: number; height: number; shape?: "box" | "round" | "pill" }
interface DiagramLink { from: string; to: string; label?: string }

const ZOOM_STEP = 0.25;
const clampZoom = (zoom: number) => Math.min(2.5, Math.max(0.5, Math.round(zoom * 100) / 100));

/** A diagram on the daemon's 600x400 canvas: boxes and arrows drawn as SVG, zoomed by the frame's controls. */
function DiagramView({ title, nodes, links }: { title: string; nodes: readonly DiagramNode[]; links: readonly DiagramLink[] }) {
  const [zoom, setZoom] = useState(1);
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const right = Math.max(...nodes.map((n) => n.x + n.width));
  const bottom = Math.max(...nodes.map((n) => n.y + n.height));
  const pad = 12;
  const centre = (n: DiagramNode) => ({ x: n.x + n.width / 2, y: n.y + n.height / 2 });
  /** Where the line from a node's centre towards `to` leaves its box. */
  const edge = (n: DiagramNode, to: { x: number; y: number }) => {
    const c = centre(n);
    const dx = to.x - c.x;
    const dy = to.y - c.y;
    if (dx === 0 && dy === 0) return c;
    const k = Math.min(dx === 0 ? Infinity : n.width / 2 / Math.abs(dx), dy === 0 ? Infinity : n.height / 2 / Math.abs(dy));
    return { x: c.x + dx * k, y: c.y + dy * k };
  };
  const marker = `arrow-${title.replace(/\W+/g, "") || "d"}`;
  return <Diagram title={title} zoom={zoom} className="max-w-xl" onZoomIn={() => setZoom((z) => clampZoom(z + ZOOM_STEP))} onZoomOut={() => setZoom((z) => clampZoom(z - ZOOM_STEP))} onReset={() => setZoom(1)}>
    <svg role="img" aria-label={`${title}: ${nodes.map((n) => n.label).join(", ")}`} width={right + pad * 2} height={bottom + pad * 2} viewBox={`${-pad} ${-pad} ${right + pad * 2} ${bottom + pad * 2}`} className="max-w-full overflow-visible">
      <defs><marker id={marker} viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M0 0L10 5L0 10z" className="fill-muted-foreground" /></marker></defs>
      {links.map((link, i) => {
        const from = byId.get(link.from);
        const to = byId.get(link.to);
        if (!from || !to) return null;
        const a = edge(from, centre(to));
        const b = edge(to, centre(from));
        return <g key={i}>
          <line x1={a.x} y1={a.y} x2={b.x} y2={b.y} className="stroke-muted-foreground" strokeWidth={1.25} markerEnd={`url(#${marker})`} />
          {link.label && <text x={(a.x + b.x) / 2} y={(a.y + b.y) / 2 - 4} textAnchor="middle" className="fill-muted-foreground text-[10px]">{link.label}</text>}
        </g>;
      })}
      {nodes.map((n) => <g key={n.id}>
        <rect x={n.x} y={n.y} width={n.width} height={n.height} rx={n.shape === "pill" ? n.height / 2 : n.shape === "round" ? 12 : 4} className="fill-card stroke-border" strokeWidth={1.25} />
        <text x={n.x + n.width / 2} y={n.y + n.height / 2} textAnchor="middle" dominantBaseline="central" className="fill-foreground text-[12px]">{n.label}</text>
      </g>)}
    </svg>
  </Diagram>;
}

/** What the daemon validated is trusted for shape, never for rendering: a throwing component shows nothing instead of taking the chat down. */
class Contain extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  componentDidCatch(error: Error, info: ErrorInfo) { console.warn("structured component failed to render", error, info.componentStack); }
  render() { return this.state.failed ? <p role="note" className="text-xs text-muted-foreground">This component could not be shown.</p> : this.props.children; }
}

function parse(spec: string | undefined): Record<string, unknown> | null {
  if (!spec) return null;
  try {
    const value: unknown = JSON.parse(spec);
    return typeof value === "object" && value !== null && !Array.isArray(value) ? (value as Record<string, unknown>) : null;
  } catch { return null; }
}

/** The document card knows whether it is the one open beside the conversation; `at` absent = a preview with nowhere to open it. */
function DocumentPart({ spec, at }: { spec: DocumentSpec; at?: PartAt }) {
  const open = useChat((s) => s.pane?.kind === "document" && at !== undefined && s.pane.messageId === at.messageId && s.pane.partIndex === at.partIndex);
  return <DocumentCard spec={spec} open={open} onOpen={at && (() => useChat.getState().openDocument(at.messageId, at.partIndex))} />;
}

function View({ kind, spec, at }: { kind: string; spec: Record<string, any>; at?: PartAt }) { // eslint-disable-line @typescript-eslint/no-explicit-any -- validated by the daemon; the element props below type the use
  switch (kind) {
    case "table": {
      const columns = spec.columns as DataTableColumn[];
      return <DataTable columns={columns} rows={spec.rows as DataTableRow[]} caption={spec.caption as string | undefined} className="max-w-full" />;
    }
    case "chart": {
      const points = spec.points as number[];
      return <Chart label={spec.label} value={spec.value} delta={spec.delta} trend={spec.trend} upIsGood={spec.upIsGood} points={points} visibleCount={points.length} variant={spec.variant} className="max-w-md" />;
    }
    case "diagram":
      return <DiagramView title={spec.title} nodes={spec.nodes as DiagramNode[]} links={(spec.links ?? []) as DiagramLink[]} />;
    case "flow":
      return <FlowGraph nodes={spec.nodes as FlowNode[]} edges={(spec.edges ?? []) as FlowEdge[]} visibleCount={(spec.nodes as FlowNode[]).length} />;
    case "math": {
      const steps = spec.steps as MathStep[];
      return <MathBlock label={spec.label} steps={steps} visibleSteps={steps.length} />;
    }
    case "spec": {
      const rows = spec.rows as SpecRow[];
      return <SpecSheet title={spec.title} subtitle={spec.subtitle} rows={rows} visibleCount={rows.length} />;
    }
    case "comparison": {
      const options = (spec.options as { id: string; name: string; headline: string; traits: (string | null)[] }[]).map((o): ComparisonOption => ({ ...o, traits: o.traits.map((t) => t ?? false) }));
      return <ComparisonCard traitLabels={spec.traitLabels} options={options} recommendedId={spec.recommendedId} reason={spec.reason} className="max-w-2xl" />;
    }
    case "timeline": {
      const events = spec.events as TimelineEvent[];
      return <div className="flex w-full max-w-sm flex-col gap-1.5">
        {spec.title && <span className="px-1 text-[13px] font-medium">{spec.title}</span>}
        <Timeline events={events} visibleCount={events.length} className="max-w-none" />
      </div>;
    }
    case "job":
      return <JobProgress title={spec.title} stages={spec.stages as JobStage[]} stageIndex={spec.stageIndex} stageProgress={spec.stageProgress} eta={spec.eta ?? ""} elapsedMs={spec.elapsedMs} outcome={spec.outcome} />;
    case "skill":
      return <SkillProposalCard spec={spec as SkillProposalSpec} />;
    case "automation":
      return <AutomationProposalCard spec={spec as AutomationProposalSpec} />;
    case "todo":
      return <TodoList title={spec.title} description={spec.description} maxVisible={spec.maxVisible} items={(spec.items ?? []) as TodoItem[]} className="max-w-md" />;
    case "document":
      return <DocumentPart spec={spec as DocumentSpec} at={at} />;
    default:
      return null;
  }
}

/** A structured component an assistant message holds (a `ui` part): the daemon's validated JSON drawn by its assistant-ui element. */
export function StructuredPart({ part, at }: { part: Pick<MsgPart, "name" | "spec">; at?: PartAt }) {
  const spec = parse(part.spec);
  if (!spec || !part.name || !(UI_KINDS as readonly string[]).includes(part.name)) return null;
  return <div data-slot="chat-structured" data-kind={part.name} className="flex min-w-0 max-w-full flex-col items-start overflow-x-auto"><Contain><View kind={part.name} spec={spec} at={at} /></Contain></div>;
}
