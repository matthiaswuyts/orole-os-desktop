import type { ReactNode } from "react";
import { Bot, Brain, ExternalLink, Info, MessageCircleQuestion, MessageSquareText, ShieldCheck } from "lucide-react";
import type { Session } from "@/mygo";
import { Markdown } from "@/components/Markdown";
import type { RunEntry } from "@/components/assistant-ui/elements/run-activity";
import { formatElapsed } from "@/components/assistant-ui/elements/working-status";
import { duration } from "@/features/agents/format";
import { describePermission } from "@/lib/permissionLabels";
import { clip, reasoningTitle, type FeedItem, type LiveActivity, type RunState } from "@/lib/taskEvents";
import { toolLabel, type ToolFileStat } from "@/lib/toolLabels";
import { PermissionItem, SourcesList, ToolDetails } from "./FeedItems";
import { approvalText, QuestionCard, SubagentCard } from "./TaskInteractions";

/** "2m 13s" below an hour (the clock people expect), "1h 05m" above. */
export const workedFor = (ms: number) => ms < 3_600_000 ? formatElapsed(Math.floor(ms / 1000)) : duration(ms);

/** The collapsed row's wording per run state; a duration is only shown when the turn's timestamps give one. */
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

const statText = (stats: readonly ToolFileStat[]) => {
  const added = stats.reduce((sum, stat) => sum + (stat.added ?? 0), 0), removed = stats.reduce((sum, stat) => sum + (stat.removed ?? 0), 0);
  return [added ? `+${added}` : "", removed ? `-${removed}` : ""].filter(Boolean).join(" ");
};

/** What a run's step row shows. `matches` gives the search excerpts of an interim message, shown under its row even while it is closed. */
export function runEntry(item: FeedItem, session: Session, matches: (key: string) => ReactNode): RunEntry | null {
  switch (item.type) {
    case "reasoning": {
      const label = reasoningTitle(item.text);
      const body = item.text.replace(/\*\*|__/g, "").trim();
      return { id: item.key, kind: "commentary", label, icon: Brain, content: body && body !== label ? <Markdown className="text-xs text-muted-foreground">{item.text}</Markdown> : undefined };
    }
    case "assistant":
      return { id: item.key, kind: "commentary", label: reasoningTitle(item.text, "Message"), icon: MessageSquareText, content: <Markdown className="text-xs">{item.text}</Markdown>, aside: matches(item.key) };
    case "tool": {
      const label = toolLabel(item.call);
      const stats = statText(label.stats);
      return {
        id: item.key, kind: "tool", label: label.verb, chip: [label.chip, stats].filter(Boolean).join(" "), icon: label.icon,
        failed: item.call.status === "error", running: item.call.status === "running", content: <ToolDetails call={item.call} />,
      };
    }
    case "sources": {
      const count = item.sources.length;
      return { id: item.key, kind: "tool", label: "Found sources", chip: `${count} ${count === 1 ? "source" : "sources"}`, icon: ExternalLink, content: <SourcesList sources={item.sources} /> };
    }
    case "subagent":
      return { id: item.key, kind: "tool", label: "Delegated to an agent", chip: item.agent.name || item.agent.id, icon: Bot, content: <SubagentCard agent={item.agent} /> };
    case "note":
      return { id: item.key, kind: "commentary", label: clip(item.text, 80) || "Note", icon: Info, content: item.text.length > 80 ? <p className="selectable whitespace-pre-wrap break-words text-xs">{item.text}</p> : undefined };
    case "permission": {
      const { capability } = describePermission(item.request);
      return { id: item.key, kind: "tool", label: item.decision ? item.decision.allow ? "Allowed" : "Denied" : "Permission closed", chip: approvalText(capability), icon: ShieldCheck, content: <PermissionItem session={session} item={item} /> };
    }
    case "question":
      return {
        id: item.key, kind: "tool", label: item.cancelled ? "Question cancelled" : item.answers ? "Answered a question" : "Question closed",
        chip: clip(item.request.questions[0]?.prompt ?? "", 80), icon: MessageCircleQuestion,
        content: <QuestionCard session={session} request={item.request} answers={item.answers} cancelled={item.cancelled} />,
      };
    default: return null;
  }
}
