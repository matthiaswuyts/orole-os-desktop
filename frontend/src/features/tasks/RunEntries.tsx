import { memo, type ReactNode } from "react";
import { Brain, ExternalLink, Info, MessageCircleQuestion, MessageSquareText, ShieldCheck, ShieldX } from "lucide-react";
import type { Session } from "@/mygo";
import { Markdown } from "@/components/Markdown";
import { AgentNode, AgentNodes, AgentStep, FadeFold, type AgentState } from "@/components/assistant-ui/elements/agent-run-tree";
import { TerminalResult } from "@/components/assistant-ui/elements/terminal-result";
import { ToolCallCard, ToolCallGroup, type ToolCallState } from "@/components/assistant-ui/elements/tool-call-card";
import { formatElapsed } from "@/components/assistant-ui/elements/working-status";
import { duration } from "@/features/agents/format";
import { describePermission } from "@/lib/permissionLabels";
import { clip, reasoningTitle, type FeedItem, type LiveActivity, type RunState, type ToolCall } from "@/lib/taskEvents";
import { shellExit, toolKind, toolLabel, toolLabelText, type ToolKind, type ToolLabel } from "@/lib/toolLabels";
import { PermissionItem, SourcesList, ToolDetails } from "./FeedItems";
import { approvalText, QuestionCard } from "./TaskInteractions";

type Item<T extends FeedItem["type"]> = Extract<FeedItem, { type: T }>;
type Subagent = Item<"subagent">;

/** "2m 13s" below an hour (the clock people expect), "1h 05m" above. */
export const workedFor = (ms: number) => ms < 3_600_000 ? formatElapsed(Math.floor(ms / 1000)) : duration(ms);

/** The settled header's wording per run state; a duration is only shown when the turn's timestamps give one. */
export function runStatusText(status: RunState, durationMs: number | null, activity: LiveActivity | null): { label: string; duration?: string } {
  const timed = durationMs === null ? undefined : workedFor(durationMs);
  switch (status) {
    case "running": return { label: activity?.label ?? "Working" };
    case "requires-action": return { label: "Needs your input" };
    case "error": return timed ? { label: "Failed after", duration: timed } : { label: "Failed" };
    case "cancelled": return timed ? { label: "Stopped after", duration: timed } : { label: "Stopped" };
    case "incomplete": return { label: "Incomplete" };
    case "complete": return timed ? { label: "Worked for", duration: timed } : { label: "Completed" };
  }
}

export const RUN_STATE: Record<RunState, AgentState> = { running: "running", "requires-action": "waiting", complete: "done", cancelled: "cancelled", error: "failed", incomplete: "cancelled" };

// Harness vocabularies, as internal/agents/manager.go subagentRunning reads them; anything else ended the agent.
const SUBAGENT_RUNNING = /^(started|running|pending|pendinginit|queued|in_progress|working)$/i;
const SUBAGENT_FAILED = /^(failed|error|errored)$/i;
const SUBAGENT_STOPPED = /^(cancel+ed|interrupted|aborted|stopped|killed)$/i;

export function subagentState(status: string): AgentState {
  if (!status) return "idle";
  if (SUBAGENT_RUNNING.test(status)) return "running";
  if (SUBAGENT_FAILED.test(status)) return "failed";
  return SUBAGENT_STOPPED.test(status) ? "cancelled" : "done";
}

/**
 * Takes the subagents whose spawning tool call is part of the run out of the step order, keyed by that call's id, so each
 * nests under its call; a subagent without a known call stays a step where it was reported.
 */
export function nestSubagents(steps: readonly FeedItem[]): { steps: FeedItem[]; spawned: ReadonlyMap<string, readonly Subagent[]> } {
  const calls = new Set(steps.flatMap((step) => step.type === "tool" ? [step.call.id] : []));
  const spawned = new Map<string, Subagent[]>();
  const rest: FeedItem[] = [];
  for (const step of steps) {
    const parent = step.type === "subagent" ? step.agent.toolCallId : "";
    if (!parent || !calls.has(parent)) { rest.push(step); continue; }
    const list = spawned.get(parent);
    if (list) list.push(step as Subagent); else spawned.set(parent, [step as Subagent]);
  }
  return { steps: rest, spawned };
}

/** One line for a step: what the run header and a plan step's latest work say. */
function stepLine(item: FeedItem): string {
  switch (item.type) {
    case "tool": return toolLabelText(toolLabel(item.call));
    case "reasoning": return reasoningTitle(item.text);
    case "assistant": return reasoningTitle(item.text, "Message");
    case "note": return clip(item.text, 80) || "Note";
    case "sources": return `Found ${item.sources.length} ${item.sources.length === 1 ? "source" : "sources"}`;
    case "subagent": return `${item.agent.name || "Agent"}: ${item.agent.message || item.agent.task || item.agent.status}`;
    case "permission": return item.decision ? item.decision.allow ? "Allowed" : "Denied" : "Permission closed";
    case "question": return clip(item.request.questions[0]?.prompt ?? "Question", 80);
    default: return "";
  }
}

/**
 * What the agent is doing now, for the header of a running run: the call in flight, else the harness's own activity line,
 * else the latest step.
 */
export function currentStep(steps: readonly FeedItem[], activity: LiveActivity | null): string {
  const call = steps.findLast((step): step is Item<"tool"> => step.type === "tool" && step.call.status === "running");
  if (call) return toolLabelText(toolLabel(call.call));
  if (activity && activity.label !== "Working…") return activity.label;
  const last = steps.at(-1);
  return (last && stepLine(last)) || activity?.label || "Working…";
}

export interface StepContext {
  session: Session;
  /** A tool permission is pending: the agent's calls in flight wait on it. */
  approval: boolean;
  spawned: ReadonlyMap<string, readonly Subagent[]>;
  /** Wraps a message in its scroll anchor and search excerpts. */
  anchor: (key: string, node: ReactNode) => ReactNode;
}

const callState = (call: ToolCall, approval: boolean): ToolCallState => call.status === "running" && approval ? "approval" : call.status;

const statText = (label: ToolLabel) => {
  const added = label.stats.reduce((sum, stat) => sum + (stat.added ?? 0), 0), removed = label.stats.reduce((sum, stat) => sum + (stat.removed ?? 0), 0);
  return [label.chip, added ? `+${added}` : "", removed ? `-${removed}` : ""].filter(Boolean).join(" ");
};

/** Tool kinds whose chip is a command, path or pattern rather than prose. */
const CODE_CHIP: Partial<Record<ToolKind, true>> = { shell: true, read: true, edit: true, files: true, image: true, "web-fetch": true };

const SubagentNode = memo(function SubagentNode({ item }: { item: Subagent }) {
  const { agent } = item;
  const state = subagentState(agent.status);
  const running = state === "running";
  const reported = Date.parse(item.at);
  return <AgentNode name={agent.name || "Agent"} state={state} summary={clip((running ? agent.message || agent.task || "Starting" : agent.message || agent.task || agent.status), 160)}
    start={running && Number.isFinite(reported) ? reported : undefined} task={agent.task || undefined}
    result={!running && agent.message ? <p className="m-0 whitespace-pre-wrap break-words">{agent.message}</p> : undefined}>
    {running && agent.message && <p className="selectable m-0 whitespace-pre-wrap break-words text-[13px]">{agent.message}</p>}
    {/* IDs and status come only from provider events; they are not Orole session links. */}
    <p className="m-0 break-all text-xs text-muted-foreground">Reported status <span className="font-mono">{agent.status || "not reported"}</span> · <span className="selectable font-mono">{agent.id}</span></p>
  </AgentNode>;
});

const subagentList = (agents: readonly Subagent[] | undefined) => agents?.length ? <AgentNodes>{agents.map((agent) => <SubagentNode key={agent.key} item={agent} />)}</AgentNodes> : null;

/**
 * A live run re-renders on every event and keeps growing, so each step only renders again when its own item, state or nested
 * agents changed; items keep their identity across regrouping (`shareItems`).
 */
interface CallProps { item: Item<"tool">; state: ToolCallState; spawned?: readonly Subagent[] }
const sameCall = (a: CallProps, b: CallProps) => a.item === b.item && a.state === b.state
  && (a.spawned === b.spawned || (a.spawned?.length === b.spawned?.length && (a.spawned ?? []).every((agent, i) => agent === b.spawned![i])));

const ToolRow = memo(function ToolRow({ item, state, spawned }: CallProps) {
  const label = toolLabel(item.call);
  return <ToolCallCard name={label.verb} summary={statText(label)} summaryMono={CODE_CHIP[label.kind]} state={state}
    error={state === "error" ? clip(item.call.output.split(/\r?\n/).find((line) => line.trim()) ?? "", 160) || "Failed" : undefined}
    start={item.call.startedAt} end={item.call.endedAt} details={<ToolDetails call={item.call} />}>
    {subagentList(spawned)}
  </ToolCallCard>;
}, sameCall);

const ShellCall = memo(function ShellCall({ item, state, spawned }: CallProps) {
  const label = toolLabel(item.call);
  const { exitCode, output } = shellExit(item.call.output);
  return <>
    <TerminalResult command={label.title || label.chip} output={output} state={state} exitCode={exitCode} start={item.call.startedAt} end={item.call.endedAt} />
    {subagentList(spawned)}
  </>;
}, sameCall);

function ToolGroup({ calls, ctx }: { calls: readonly Item<"tool">[]; ctx: StepContext }) {
  const states = calls.map((item) => callState(item.call, ctx.approval));
  const rows = calls.map((item, i) => <ToolRow key={item.key} item={item} state={states[i]!} spawned={ctx.spawned.get(item.call.id)} />);
  if (calls.length < 2) return <ToolCallGroup>{rows}</ToolCallGroup>;
  const waiting = states.lastIndexOf("approval"), active = states.lastIndexOf("running");
  const now = waiting >= 0 ? waiting : active;
  if (now >= 0) {
    const { call } = calls[now]!;
    const label = toolLabel(call);
    return <ToolCallGroup state={states[now]} title={waiting >= 0 ? "Waiting for approval" : label.verb} subject={waiting >= 0 ? toolLabelText(label) : label.chip} start={call.startedAt}>{rows}</ToolCallGroup>;
  }
  const failed = states.filter((state) => state === "error").length;
  const ends = calls.map((item) => item.call.endedAt);
  const end = ends.every((at) => at !== undefined) ? Math.max(...(ends as number[])) : undefined;
  return <ToolCallGroup state={failed ? "error" : states.every((state) => state === "ok") ? "ok" : "unknown"} title={`${calls.length} tool calls`} subject={failed ? `${failed} failed` : undefined}
    start={calls[0]!.call.startedAt} end={end}>{rows}</ToolCallGroup>;
}

interface StepProps { item: FeedItem; session: Session; anchor: StepContext["anchor"] }

/** Every step that is not a tool call. Only permission and question steps read the session. */
const Step = memo(function Step({ item, session, anchor }: StepProps) {
  switch (item.type) {
    case "reasoning": {
      const label = reasoningTitle(item.text);
      const body = item.text.replace(/\*\*|__/g, "").trim();
      return <AgentStep icon={<Brain />} label={label} details={body && body !== label ? <Markdown className="text-xs text-muted-foreground">{item.text}</Markdown> : undefined} />;
    }
    case "assistant":
      return anchor(item.key, <AgentStep icon={<MessageSquareText />}>
        <p className="sr-only">Agent said</p>
        <FadeFold foldable={item.text.length > 400 || item.text.split("\n").length > 6} collapsedClassName="max-h-28"><Markdown className="text-[13px]">{item.text}</Markdown></FadeFold>
      </AgentStep>);
    case "note":
      return <AgentStep icon={<Info />} label={clip(item.text, 120) || "Note"} details={item.text.length > 120 ? <p className="selectable m-0 whitespace-pre-wrap break-words text-xs">{item.text}</p> : undefined} />;
    case "sources":
      return <AgentStep icon={<ExternalLink />} label={stepLine(item)} details={<SourcesList sources={item.sources} />} />;
    case "permission": {
      const { capability } = describePermission(item.request);
      return <AgentStep icon={item.decision && !item.decision.allow ? <ShieldX /> : <ShieldCheck />} label={`${stepLine(item)} · ${approvalText(capability)}`} details={<PermissionItem session={session} item={item} />} />;
    }
    case "question":
      return <AgentStep icon={<MessageCircleQuestion />} label={`${item.cancelled ? "Question cancelled" : item.answers ? "Answered a question" : "Question closed"} · ${stepLine(item)}`}
        details={<QuestionCard session={session} request={item.request} answers={item.answers} cancelled={item.cancelled} />} />;
    case "subagent":
      return subagentList([item]);
    default: return null;
  }
}, (a, b) => a.item === b.item && a.anchor === b.anchor && (a.session === b.session || (b.item.type !== "permission" && b.item.type !== "question")));

/**
 * A run's steps in recorded order: thoughts and notes as one-line steps, messages the agent wrote while working as text,
 * consecutive tool calls as one card of rows, shell commands as terminal results, and subagents under the call that started them.
 */
export function RunSteps({ items, ctx }: { items: readonly FeedItem[]; ctx: StepContext }) {
  const out: ReactNode[] = [];
  let group: Item<"tool">[] = [];
  const flush = () => {
    if (group.length) out.push(<ToolGroup key={group[0]!.key} calls={group} ctx={ctx} />);
    group = [];
  };
  for (const item of items) {
    if (item.type === "tool") {
      if (toolKind(item.call.name, item.call.server) !== "shell") { group.push(item); continue; }
      flush();
      out.push(<ShellCall key={item.key} item={item} state={callState(item.call, ctx.approval)} spawned={ctx.spawned.get(item.call.id)} />);
      continue;
    }
    flush();
    out.push(<Step key={item.key} item={item} session={ctx.session} anchor={ctx.anchor} />);
  }
  flush();
  return out.length ? <div className="flex min-w-0 flex-col gap-1.5">{out}</div> : null;
}

/** A plan step's latest piece of work, on one line under the step while it is in progress. */
export function LatestStep({ item, live }: { item: FeedItem; live: boolean }) {
  const Icon = item.type === "tool" ? toolLabel(item.call).icon : item.type === "reasoning" ? Brain : item.type === "assistant" ? MessageSquareText : Info;
  return <AgentStep icon={<Icon />} label={stepLine(item)} live={live} />;
}
