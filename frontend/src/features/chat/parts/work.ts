import { FileTextIcon, GlobeIcon, SearchIcon, TimerIcon, WrenchIcon, BrainIcon, type LucideIcon } from "lucide-react";
import type { MsgPart } from "@/mygo";
import type { ReportSection } from "@/components/assistant-ui/elements/research-report";
import type { TimelineStep } from "@/components/assistant-ui/elements/tool-timeline";
import { hostOf, stepState, toolLabel, type Source } from "./format";

const TOOL_ICONS: Record<string, LucideIcon> = {
  web_search: SearchIcon, web_fetch: GlobeIcon, datetime: TimerIcon, research: FileTextIcon, knowledge_search: SearchIcon,
  knowledge_write: FileTextIcon, memory: BrainIcon, memory_search: BrainIcon, search_history: SearchIcon,
};

/** Rows of the research tool carry ids like "research:q2"; every other tool or delegation row is plain work. */
const isResearchRow = (part: MsgPart): boolean => part.type === "tool" && part.name === "research" && (part.id ?? "").startsWith("research:");

/** What the assistant did in one message, ready for the timeline, web search and research outline elements. */
export interface Work {
  steps: TimelineStep[];
  /** the research outline, when the research tool ran */
  research: { title: string; sections: ReportSection[]; sourcesRead: number } | null;
  /** the latest web search query, "" when there was none */
  query: string;
  /** a web search or page read is in flight */
  searching: boolean;
  /** bots this bot handed work to, newest last */
  handoffs: { id: string; to: string; botId?: string; request: string; /** the answer once done, or why it failed */ outcome: string; state: "running" | "done" | "error" | "stopped" }[];
  /** how many things happened, for the collapsed label */
  count: number;
  /** nothing in the message can still change */
  streaming: boolean;
}

const SOURCES_SUFFIX = /^(.*) \((\d+) sources?\)$/;

/** The research outline from the tool's progress rows: one section per sub-question, then the report itself. */
function researchOutline(rows: readonly MsgPart[], active: boolean): Work["research"] {
  const byId = new Map(rows.map((row) => [(row.id ?? "").slice("research:".length), row]));
  const sections: ReportSection[] = [];
  for (const [id, row] of byId) {
    if (!/^q\d+$/.test(id)) continue;
    const state = stepState(row, active);
    const match = SOURCES_SUFFIX.exec(row.summary ?? "");
    sections.push({
      id, heading: match?.[1] ?? row.summary ?? id, sources: match ? Number(match[2]) : 0,
      state: state === "done" ? "done" : state === "running" ? "writing" : "pending",
      ...(state === "error" ? { preview: "Could not be researched" } : {}),
    });
  }
  const synthesis = byId.get("synthesis");
  if (synthesis) {
    const state = stepState(synthesis, active);
    sections.push({ id: "synthesis", heading: "Write the report", sources: 0, state: state === "done" ? "done" : state === "running" ? "writing" : "pending" });
  }
  const sourcesRow = byId.get("sources");
  return { title: "Research", sections, sourcesRead: Number(/^(\d+)/.exec(sourcesRow?.summary ?? "")?.[1] ?? 0) };
}

/** Splits a message's parts into the work the timeline shows; everything else is the answer. */
export function workOf(parts: readonly MsgPart[], active: boolean): Work {
  const research = parts.filter(isResearchRow);
  const plain = parts.filter((p) => (p.type === "tool" || p.type === "delegation") && !isResearchRow(p) && p.name !== "ask_user" && p.name !== "ask_bot" && !p.name?.startsWith("show_"));
  // A delegation is drawn as a handoff card, not as a step.
  const steps: TimelineStep[] = plain.filter((part) => part.type !== "delegation").map((part, i) => {
    const state = stepState(part, active);
    return { id: part.id || `t${i}`, verb: toolLabel(part.name, state), chip: part.summary ?? "", icon: TOOL_ICONS[part.name ?? ""] ?? WrenchIcon };
  });
  const outline = research.length ? researchOutline(research, active) : null;
  const searches = plain.filter((p) => p.name === "web_search");
  const reads = plain.filter((p) => p.name === "web_search" || p.name === "web_fetch");
  const handoffs = plain.filter((p) => p.type === "delegation").map((p, i) => ({
    id: p.id || `h${i}`, to: p.name?.trim() || "another bot", botId: p.botId, state: stepState(p, active),
    // Older rows only have the one-line summary, which is the request while it runs.
    request: p.text || (stepState(p, active) === "running" ? p.summary ?? "" : ""),
    outcome: stepState(p, active) === "running" ? "" : p.summary ?? "",
  }));
  return {
    steps, research: outline, handoffs,
    query: searches.at(-1)?.summary ?? "",
    searching: reads.some((p) => stepState(p, active) === "running"),
    count: steps.length + handoffs.length + (outline ? 1 : 0),
    streaming: active && (steps.length === 0 || [...plain, ...research].some((p) => stepState(p, active) === "running")),
  };
}

/** A source as a web search result row. */
export const asResult = (source: Source) => ({ title: source.title, domain: hostOf(source.url) });
