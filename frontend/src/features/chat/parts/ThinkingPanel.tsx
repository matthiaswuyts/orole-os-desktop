import { CheckIcon, ChevronDownIcon, CircleDashedIcon, GlobeIcon, MinusIcon, XIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { AgentHandoff } from "@/components/assistant-ui/elements/agent-handoff";
import { ResearchReport, type ReportSection } from "@/components/assistant-ui/elements/research-report";
import { bubble } from "@/components/assistant-ui/elements/surfaces";
import { formatElapsed } from "@/components/assistant-ui/elements/working-status";
import { BotAvatar } from "@/components/shell/BotAvatar";
import { Button } from "@/components/ui/button";
import { isWebURL, openExternal } from "@/lib/openExternal";
import { cn } from "@/lib/utils";
import type { Message, MsgPart } from "@/mygo";
import { hostOf, isActive, stepState, toolLabel, type Source, type StepState } from "./format";

/** The few bot fields a handoff needs to draw a bot. */
export interface BotRef { id: string; name: string; avatar: string }

/** One line of the panel: what the bot did or is doing, in the order it happened. */
export interface ThoughtStep {
  key: string;
  label: string;
  state: StepState;
  /** what the model was thinking, or the query/target of a tool */
  text?: string;
  kind: "thinking" | "tool" | "search" | "research" | "handoff" | "answer";
  /** the part (delegation) or rows (research) the step draws */
  parts: MsgPart[];
}

/** Tool rows that are drawn elsewhere: a question card (ask_user) or the handoff its delegation row draws (ask_bot). */
const HIDDEN_TOOLS: Record<string, true> = { ask_user: true, ask_bot: true };

/**
 * What a reply did, step by step and in order: its thinking (consecutive reasoning parts make one step), every tool call,
 * the research outline, each handoff to another bot, and finally writing the answer. While the reply runs, the newest
 * thinking is the running step until something follows it, and an empty gap after finished steps reads as thinking.
 */
export function thoughtSteps(message: Pick<Message, "parts" | "status">): ThoughtStep[] {
  const active = isActive(message.status);
  const steps: ThoughtStep[] = [];
  const { parts } = message;
  parts.forEach((part, i) => {
    const last = steps.at(-1);
    switch (part.type) {
      case "reasoning": {
        const text = part.text?.trim();
        if (!text) return;
        if (last?.kind === "thinking") { last.text = `${last.text}\n\n${text}`; return; }
        steps.push({ key: `r${i}`, kind: "thinking", label: "Thinking", text, state: "done", parts: [part] });
        return;
      }
      case "tool": {
        if (HIDDEN_TOOLS[part.name ?? ""]) return;
        if (part.name === "research" && (part.id ?? "").startsWith("research:")) {
          const research = steps.find((s) => s.kind === "research");
          if (research) research.parts.push(part);
          else steps.push({ key: `q${i}`, kind: "research", label: "", state: "done", parts: [part] });
          return;
        }
        const state = stepState(part, active);
        const search = part.name === "web_search" || part.name === "web_fetch";
        steps.push({ key: part.id || `t${i}`, kind: search ? "search" : "tool", label: toolLabel(part.name, state), text: part.summary, state, parts: [part] });
        return;
      }
      case "delegation": {
        const state = stepState(part, active);
        const name = part.name?.trim() || "another bot";
        steps.push({ key: part.id || `d${i}`, kind: "handoff", label: state === "running" ? `Asking ${name}` : `Asked ${name}`, state, parts: [part] });
        return;
      }
      case "text":
        if (!part.hidden && part.text && last?.kind !== "answer") steps.push({ key: `a${i}`, kind: "answer", label: "Writing the answer", state: "done", parts: [] });
        return;
      default:
        return;
    }
  });
  for (const step of steps) {
    if (step.kind !== "research") continue;
    const states = step.parts.map((row) => stepState(row, active));
    step.state = states.includes("running") ? "running" : states.includes("error") && !states.includes("done") ? "error" : states.includes("stopped") ? "stopped" : "done";
    step.label = step.state === "running" ? "Researching" : "Researched";
  }
  const last = steps.at(-1);
  if (active) {
    // Thinking and writing have no row status of their own: the newest one is what is happening now.
    if (last && (last.kind === "thinking" || last.kind === "answer")) last.state = "running";
    else if (!steps.some((step) => step.state === "running")) steps.push({ key: "now", kind: "thinking", label: message.status === "pending" ? "Starting" : "Thinking", state: "running", parts: [] });
  } else if (message.status !== "complete" && last && (last.kind === "thinking" || last.kind === "answer")) {
    last.state = "stopped";
  }
  return steps;
}

const SOURCES_SUFFIX = /^(.*) \((\d+) sources?\)$/;

/** The research outline from the tool's progress rows: one section per sub-question, then the report itself. */
function researchOutline(rows: readonly MsgPart[], active: boolean): { sections: ReportSection[]; sourcesRead: number } {
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
  return { sections, sourcesRead: Number(/^(\d+)/.exec(byId.get("sources")?.summary ?? "")?.[1] ?? 0) };
}

/** The glyph of a step: a check when done, a turning dashed ring while running, a cross when it failed, a dash when it never finished. */
function StateIcon({ state, className }: { state: StepState; className?: string }) {
  switch (state) {
    case "done": return <CheckIcon aria-hidden className={cn("size-4 text-success", className)} />;
    case "running": return <CircleDashedIcon aria-hidden className={cn("size-4 animate-spin text-muted-foreground [animation-duration:2.4s] motion-reduce:animate-none", className)} />;
    case "error": return <XIcon aria-hidden className={cn("size-4 text-destructive", className)} />;
    case "stopped": return <MinusIcon aria-hidden className={cn("size-4 text-muted-foreground", className)} />;
  }
}

/** Whole seconds since `since` (Unix ms), ticking once a second while `live`. */
function useSeconds(since: number | undefined, live: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!live) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [live]);
  return since ? Math.max(0, Math.floor((now - since) / 1000)) : 0;
}

/** At most this many sources show as chips under a search; the rest are counted. */
const SOURCE_CHIPS = 4;

/**
 * What the bot is doing, live, as a chat bubble: the step running now in the header with the time so far, and every step
 * of the reply in order below it (its thinking, each tool call, the pages it read, handoffs, writing the answer). Open
 * while the reply runs; once it settles it folds to "Thought for 7s", and opens again on click. A reply that only wrote
 * an answer leaves nothing behind.
 */
export function ThinkingPanel({ message, sources, botName, botAvatar, bots, onOpenBot }: {
  message: Pick<Message, "parts" | "status"> & Partial<Pick<Message, "createdAt" | "finishedAt">>;
  sources: readonly Source[];
  botName: string;
  botAvatar: string;
  bots: readonly BotRef[];
  onOpenBot?: (botId: string) => void;
}) {
  const active = isActive(message.status);
  const steps = thoughtSteps(message);
  const [open, setOpen] = useState<boolean | null>(null);
  const seconds = useSeconds(message.createdAt, active);
  const work = steps.filter((step) => step.kind !== "answer");
  if (!active && work.length === 0) return null;
  const expanded = open ?? active;
  const current = steps.findLast((step) => step.state === "running");
  const took = message.finishedAt && message.createdAt ? Math.max(0, Math.round((message.finishedAt - message.createdAt) / 1000)) : undefined;
  const stopped = !active && message.status !== "complete";
  const title = active ? current?.label ?? "Thinking"
    : stopped ? "Stopped"
      : took !== undefined ? `Thought for ${formatElapsed(took)}`
        : `${work.length} ${work.length === 1 ? "step" : "steps"}`;
  const lastSearch = steps.findLast((step) => step.kind === "search");

  return <div data-slot="thinking-panel" data-state={active ? "running" : stopped ? "stopped" : "done"} className={cn(bubble, "w-full max-w-xl min-w-0 p-1.5")}>
    <button type="button" aria-expanded={expanded} onClick={() => setOpen(!expanded)}
      className="flex w-full min-w-0 items-center gap-2.5 rounded-xl p-1 text-start outline-none transition-colors hover:bg-foreground/4 focus-visible:ring-2 focus-visible:ring-ring">
      <span className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-foreground/6">
        <StateIcon state={active ? "running" : stopped ? "stopped" : "done"} />
      </span>
      <span role="status" aria-live="polite" className="min-w-0 flex-1 truncate text-sm font-medium">{title}</span>
      {active && seconds > 0 && <span data-slot="thinking-elapsed" className="shrink-0 text-xs text-muted-foreground tabular-nums">{formatElapsed(seconds)}</span>}
      <ChevronDownIcon aria-hidden className={cn("size-4 shrink-0 text-muted-foreground transition-transform duration-200 motion-reduce:transition-none", !expanded && "-rotate-90")} />
    </button>
    {expanded && <ol aria-label="Steps" className="selectable mt-1.5 flex flex-col px-1 pb-1">
      {steps.map((step, index) => <li key={step.key} data-slot="thinking-step" data-state={step.state} className="relative flex min-w-0 gap-2.5 pb-3 last:pb-0">
        <span aria-hidden className="relative flex w-7 shrink-0 justify-center pt-0.5">
          <StateIcon state={step.state} />
          {index < steps.length - 1 && <span className="absolute top-6 bottom-[-0.25rem] w-px bg-foreground/12" />}
        </span>
        <div className="flex min-w-0 flex-1 flex-col gap-1.5">
          <p className={cn("text-sm leading-5", step.state === "running" ? "text-foreground" : "text-foreground/90")}>{step.label}</p>
          {step.text && step.kind === "thinking" && <p className={cn("whitespace-pre-wrap break-words text-[13px] leading-5 text-muted-foreground", step.state !== "running" && "line-clamp-4")}>
            {/* The model's newest thought is what matters while it thinks; earlier ones scroll out of the step. */}
            {step.state === "running" && step.text.length > 600 ? `…${step.text.slice(-600)}` : step.text}
          </p>}
          {step.text && (step.kind === "tool" || step.kind === "search") && <p className="truncate text-[13px] leading-5 text-muted-foreground">{step.text}</p>}
          {step === lastSearch && sources.length > 0 && <SourceChips sources={sources} />}
          {step.kind === "research" && <ResearchSection rows={step.parts} active={active} />}
          {step.kind === "handoff" && step.parts[0] && <Handoff part={step.parts[0]} active={active} botName={botName} botAvatar={botAvatar} bots={bots} onOpenBot={onOpenBot} />}
        </div>
      </li>)}
    </ol>}
  </div>;
}

/** The pages a search read, as chips that open them; only http(s) links open. */
function SourceChips({ sources }: { sources: readonly Source[] }) {
  return <div className="flex flex-wrap items-center gap-1.5">
    {sources.slice(0, SOURCE_CHIPS).map((source) => {
      const chip = <><GlobeIcon aria-hidden className="size-3.5 shrink-0 text-muted-foreground" /><span className="truncate">{source.title}</span></>;
      const className = "inline-flex h-6 max-w-56 min-w-0 items-center gap-1.5 rounded-full bg-background/60 px-2 text-xs";
      return isWebURL(source.url)
        ? <button key={source.url} type="button" className={cn(className, "hover:bg-background focus-visible:ring-2 focus-visible:ring-ring outline-none")} aria-label={`${source.title} (${hostOf(source.url)})`} onClick={() => void openExternal(source.url)}>{chip}</button>
        : <span key={source.url} className={className}>{chip}</span>;
    })}
    {sources.length > SOURCE_CHIPS && <span className="text-xs text-muted-foreground">+{sources.length - SOURCE_CHIPS} more</span>}
  </div>;
}

function ResearchSection({ rows, active }: { rows: readonly MsgPart[]; active: boolean }) {
  const { sections, sourcesRead } = researchOutline(rows, active);
  return sections.length > 0 ? <ResearchReport title="Research" sections={sections} sourcesRead={sourcesRead} /> : null;
}

/** A handoff to another bot: who asked whom, what was asked and what came back, with a way to that bot's chat. */
function Handoff({ part, active, botName, botAvatar, bots, onOpenBot }: { part: MsgPart; active: boolean; botName: string; botAvatar: string; bots: readonly BotRef[]; onOpenBot?: (botId: string) => void }) {
  const state = stepState(part, active);
  const to = part.name?.trim() || "another bot";
  const callee = bots.find((b) => b.id === part.botId);
  // Older rows only have the one-line summary, which is the request while it runs.
  const request = part.text || (state === "running" ? part.summary ?? "" : "");
  const outcome = state === "running" ? "" : part.summary ?? "";
  const quote = "selectable line-clamp-4 min-w-0 whitespace-pre-wrap break-words border-s-2 ps-3 text-[13px] text-muted-foreground";
  return <div className="flex min-w-0 flex-col items-start gap-1.5">
    <AgentHandoff from={botName} to={to} fromAvatar={<BotAvatar name={botName} avatar={botAvatar} className="size-4 text-[9px]" />}
      toAvatar={<BotAvatar name={to} avatar={callee?.avatar ?? ""} className="size-4 text-[9px]" />}
      reason={`Handoff by ${botName}`} carried={[]} settled={state !== "running"} />
    {request && <p className={quote}><span className="font-medium text-foreground">{botName} asked: </span>{request}</p>}
    {outcome && <p className={cn(quote, state === "error" && "border-destructive/50")}><span className={cn("font-medium", state === "error" ? "text-destructive-foreground" : "text-foreground")}>{state === "error" ? `${to} could not answer: ` : `${to} answered: `}</span>{outcome}</p>}
    {part.botId && onOpenBot && <Button type="button" variant="outline" size="xs" onClick={() => onOpenBot(part.botId!)}>Open {to}'s chat</Button>}
  </div>;
}
