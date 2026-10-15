import { useMemo, useState } from "react";
import { BotIcon, CheckIcon, ChevronRight, CircleSlashIcon, CircleXIcon, LoaderCircleIcon } from "lucide-react";
import type { Event as AgentEvent } from "@/mygo";
import type { AgentState } from "@/components/assistant-ui/elements/agent-run-tree";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { dataOf } from "@/lib/taskEvents";
import { cn } from "@/lib/utils";
import { duration, useNow } from "@/features/agents/format";
import { subagentState, workedFor } from "../RunEntries";
import { RailSection } from "./RailSection";

export interface BackgroundTask {
  id: string;
  name: string;
  task: string;
  message: string;
  state: AgentState;
  /** First and latest report, epoch ms. */
  startedAt: number;
  updatedAt: number;
}

/**
 * One entry per background agent (subagent, child task) the harness reported, folded from its `subagent` events: the latest
 * status and text, a field a later report leaves empty keeping its earlier value, and when it was first and last reported.
 */
export function backgroundTasks(events: readonly AgentEvent[]): BackgroundTask[] {
  const byId = new Map<string, BackgroundTask>();
  for (const event of events) {
    if (event.kind !== "subagent") continue;
    const data = dataOf(event);
    const text = (key: string) => typeof data[key] === "string" ? data[key] : "";
    const id = text("id");
    if (!id) continue;
    const at = Date.parse(event.at);
    const previous = byId.get(id);
    byId.set(id, {
      id,
      name: text("name") || previous?.name || "",
      task: text("task") || previous?.task || "",
      message: text("message") || previous?.message || "",
      state: subagentState(text("status")),
      startedAt: previous?.startedAt ?? at,
      updatedAt: at,
    });
  }
  return [...byId.values()];
}

const ACTIVE: Partial<Record<AgentState, true>> = { running: true, waiting: true, idle: true };

/**
 * The task's background agents, after the agent-list pattern: failed ones lifted to the top, then the ones still running
 * with their time so far, and the finished ones folded behind one "N finished" row. Built from the loaded history only.
 */
export function BackgroundTasksSection({ events }: { events: readonly AgentEvent[] }) {
  const [showFinished, setShowFinished] = useState(false);
  const all = useMemo(() => backgroundTasks(events), [events]);
  const failed = all.filter((task) => task.state === "failed").toSorted((a, b) => b.updatedAt - a.updatedAt);
  const active = all.filter((task) => ACTIVE[task.state]).toSorted((a, b) => b.startedAt - a.startedAt);
  const finished = all.filter((task) => task.state === "done" || task.state === "cancelled").toSorted((a, b) => b.updatedAt - a.updatedAt);
  const now = useNow(active.length > 0);
  const summary = active.length ? `${active.length} running` : all.length ? `${all.length} finished` : undefined;
  return <RailSection id="background" title="Background tasks" summary={summary}>
    {!all.length ? <p className="text-xs text-muted-foreground">No background agents in this task.</p>
      : <ul aria-label="Background tasks" className="flex flex-col">
        {[...failed, ...active].map((task) => <TaskRow key={task.id} task={task} now={now} />)}
        {finished.length > 0 && <li>
          <button type="button" aria-expanded={showFinished} onClick={() => setShowFinished(!showFinished)} className="flex min-h-7 w-full items-center gap-2 rounded text-left text-[13px] text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring">
            <BotIcon aria-hidden className="size-3.5 shrink-0" />
            {finished.length} finished
            <ChevronRight aria-hidden className={cn("size-3 shrink-0 transition-transform motion-reduce:transition-none", showFinished && "rotate-90")} />
          </button>
        </li>}
        {showFinished && finished.map((task) => <TaskRow key={task.id} task={task} now={now} />)}
      </ul>}
  </RailSection>;
}

function StatusIcon({ state }: { state: AgentState }) {
  const base = "mt-0.5 size-3.5 shrink-0";
  switch (state) {
    case "running": return <LoaderCircleIcon aria-label="Running" className={cn(base, "animate-spin text-info motion-reduce:animate-none")} />;
    case "failed": return <CircleXIcon aria-label="Failed" className={cn(base, "text-destructive")} />;
    case "done": return <CheckIcon aria-label="Finished" className={cn(base, "text-success")} />;
    case "cancelled": return <CircleSlashIcon aria-label="Stopped" className={cn(base, "text-muted-foreground")} />;
    default: return <BotIcon aria-label="Waiting" className={cn(base, "text-muted-foreground")} />;
  }
}

function TaskRow({ task, now }: { task: BackgroundTask; now: number }) {
  const running = ACTIVE[task.state] === true;
  const name = task.name || task.task || "Agent";
  const detail = task.state === "failed" ? `Failed${task.message ? `: ${task.message}` : ""}`
    : task.state === "cancelled" ? "Stopped"
      : running ? task.message || (task.name ? task.task : "") || "Running now"
        : task.message || (task.name ? task.task : "") || "Finished";
  const time = running
    ? Number.isFinite(task.startedAt) ? workedFor(now - task.startedAt) : ""
    : Number.isFinite(task.updatedAt) ? duration(now - task.updatedAt) : "";
  const row = <>
    <StatusIcon state={task.state} />
    <span className="flex min-w-0 flex-1 flex-col">
      <span className="truncate text-[13px]">{name}</span>
      <span className={cn("truncate text-xs text-muted-foreground", task.state === "failed" && "text-destructive-foreground")}>{detail}</span>
    </span>
    {time && <span className="shrink-0 text-xs text-muted-foreground tabular-nums">{time}</span>}
  </>;
  // Name and detail are cut to one line each; the full task and report stay readable on hover and focus.
  const full = [task.task !== name ? task.task : "", task.message].filter(Boolean).join("\n\n");
  if (!full) return <li className="flex min-w-0 items-start gap-2 py-1">{row}</li>;
  return <Tooltip>
    <TooltipTrigger render={<li tabIndex={0} className="flex min-w-0 items-start gap-2 rounded py-1 outline-none focus-visible:ring-2 focus-visible:ring-ring" />}>{row}</TooltipTrigger>
    <TooltipPopup side="left" className="max-w-80"><span className="line-clamp-12 whitespace-pre-wrap break-words">{full}</span></TooltipPopup>
  </Tooltip>;
}
