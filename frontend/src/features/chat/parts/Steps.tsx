import { BotIcon, BrainIcon, CheckIcon, CircleAlertIcon, CircleSlashIcon, FileTextIcon, GlobeIcon, InfoIcon, Loader2Icon, SearchIcon, TimerIcon, WrenchIcon } from "lucide-react";
import type { ComponentType } from "react";
import { Collapsible, CollapsiblePanel, CollapsibleTrigger } from "@/components/ui/collapsible";
import { cn } from "@/lib/utils";
import { formatDuration, stepState, toolLabel, type StepState } from "./format";

const TOOL_ICONS: Record<string, ComponentType<{ className?: string }>> = {
  web_search: SearchIcon, web_fetch: GlobeIcon, datetime: TimerIcon, research: FileTextIcon, knowledge_search: SearchIcon,
  memory: BrainIcon, memory_search: BrainIcon, search_history: SearchIcon,
};

function StateGlyph({ state }: { state: StepState }) {
  if (state === "running") return <Loader2Icon aria-hidden className="size-3 animate-spin motion-reduce:animate-none" />;
  if (state === "error") return <CircleAlertIcon aria-hidden className="size-3 text-destructive" />;
  if (state === "stopped") return <CircleSlashIcon aria-hidden className="size-3" />;
  return <CheckIcon aria-hidden className="size-3 text-success" />;
}

function Step({ icon: Icon, label, state, durationMs, summary }: {
  icon: ComponentType<{ className?: string }>; label: string; state: StepState; durationMs?: number; summary?: string;
}) {
  const detail = <span className="flex min-w-0 items-center gap-1.5">
    <Icon aria-hidden className="size-3.5 shrink-0" />
    <span className={cn("shrink-0 font-medium", state === "error" && "text-destructive")}>{label}</span>
    {durationMs ? <span className="shrink-0 tabular-nums">{formatDuration(durationMs)}</span> : null}
    {summary && <span className="min-w-0 truncate" title={summary}>{summary}</span>}
    <StateGlyph state={state} />
  </span>;
  return <li data-slot="chat-step" data-state={state} className="flex min-w-0 text-xs text-muted-foreground">
    <span className="sr-only">{state === "running" ? "In progress: " : ""}</span>
    {detail}
  </li>;
}

/** One tool activity row: "Searched the web · 1.2 s · query". */
export function ToolRow({ name, status, summary, durationMs, active }: { name?: string; status?: string; summary?: string; durationMs?: number; active: boolean }) {
  const state = stepState({ status }, active);
  return <Step icon={TOOL_ICONS[name ?? ""] ?? WrenchIcon} label={toolLabel(name, state)} state={state} durationMs={durationMs} summary={summary} />;
}

/** One ask_bot step: "Asked <bot>". */
export function DelegationRow({ name, status, summary, durationMs, active }: { name?: string; status?: string; summary?: string; durationMs?: number; active: boolean }) {
  const state = stepState({ status }, active);
  const who = name?.trim() || "another bot";
  const label = state === "running" ? `Asking ${who}` : state === "error" ? `Asking ${who} failed` : state === "stopped" ? `Asking ${who} did not finish` : `Asked ${who}`;
  return <Step icon={BotIcon} label={label} state={state} durationMs={durationMs} summary={summary} />;
}

/** A list wrapper so consecutive steps read as one group of activity. */
export function StepList({ children }: { children: React.ReactNode }) {
  return <ul aria-label="Activity" className="flex min-w-0 list-none flex-col gap-1 p-0">{children}</ul>;
}

/** Collapsed "Thought for 4.2 s"; the text opens on demand and is never rendered as markup. */
export function ReasoningBlock({ text, running, durationMs }: { text: string; running: boolean; durationMs?: number }) {
  const title = running ? "Thinking…" : durationMs ? `Thought for ${formatDuration(durationMs)}` : "Thought process";
  return <Collapsible data-slot="chat-reasoning" className="min-w-0 text-xs text-muted-foreground">
    <CollapsibleTrigger className="group/reasoning flex items-center gap-1.5 rounded-md py-0.5 outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring">
      {running ? <Loader2Icon aria-hidden className="size-3 animate-spin motion-reduce:animate-none" /> : <BrainIcon aria-hidden className="size-3.5" />}
      <span className="font-medium">{title}</span>
    </CollapsibleTrigger>
    <CollapsiblePanel>
      <p className="selectable mt-1 whitespace-pre-wrap border-l-2 pl-3 leading-relaxed">{text}</p>
    </CollapsiblePanel>
  </Collapsible>;
}

/** Engine notices (compaction, budget stops): informational, not errors. */
export function NoticeLine({ text }: { text: string }) {
  return <p role="note" data-slot="chat-notice" className="flex items-start gap-1.5 text-xs text-muted-foreground">
    <InfoIcon aria-hidden className="mt-0.5 size-3.5 shrink-0" />
    <span className="selectable min-w-0 break-words">{text}</span>
  </p>;
}

/** "Memory updated" chip for a verified memory write. */
export function MemoryChip({ text }: { text?: string }) {
  return <p data-slot="chat-memory" className="flex min-w-0 max-w-full items-center gap-1.5 self-start rounded-full border bg-muted/40 px-2 py-0.5 text-xs text-muted-foreground">
    <BrainIcon aria-hidden className="size-3.5 shrink-0" />
    <span className="shrink-0 font-medium">Memory updated</span>
    {text && <span className="min-w-0 truncate" title={text}>{text}</span>}
  </p>;
}
