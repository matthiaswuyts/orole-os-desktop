import { CheckIcon, CircleIcon, CircleXIcon, LoaderCircleIcon, TriangleAlertIcon } from "lucide-react";
import { SubagentList, type SubagentItem, type SubagentStatus } from "@/components/assistant-ui/elements/subagent-list";
import type { AgentsStatus, MsgPart, Session } from "@/mygo";
import { ProviderLogo } from "@/features/agents/ProviderLogo";
import { kindLabel, sessionTitle } from "@/features/agents/format";
import { cn } from "@/lib/utils";
import { useChat } from "@/stores/chat";
import { TASK_STATUS_LABEL, useTaskSessions } from "../taskSession";

/** Spinner while the agent works, a check when its turn is done, a warning when it needs you, a cross when it failed. */
export function TaskStatusGlyph({ status, className }: { /** absent while the task is still loading */ status?: AgentsStatus; className?: string }) {
  const base = cn("size-3.5 shrink-0", className);
  switch (status) {
    case undefined:
    case "working":
      return <LoaderCircleIcon aria-hidden className={cn(base, "animate-spin motion-reduce:animate-none", status ? "text-info" : "text-muted-foreground")} />;
    case "done":
      return <CheckIcon aria-hidden className={cn(base, "text-success")} />;
    case "needs-you":
      return <TriangleAlertIcon aria-hidden className={cn(base, "text-warning")} />;
    case "failed":
      return <CircleXIcon aria-hidden className={cn(base, "text-destructive")} />;
    default:
      return <CircleIcon aria-hidden className={cn(base, "text-muted-foreground")} />;
  }
}

/** The coding agent a task runs on, as a small logo-and-name badge. */
export function HarnessBadge({ kind }: { kind: string }) {
  return <span data-slot="harness-badge" className="inline-flex shrink-0 items-center gap-1 rounded-md bg-muted px-1.5 py-0.5 text-[11px] leading-none text-muted-foreground [&>span]:size-3 [&>svg]:size-3">
    <ProviderLogo kind={kind} />{kindLabel(kind)}
  </span>;
}

const ROW_STATUS: Record<AgentsStatus, SubagentStatus> = { idle: "running", working: "running", "needs-you": "needs-you", done: "done", failed: "failed", stopped: "failed", lost: "failed" };

/** What a task's row says under its title: the agent's current activity, or how many background agents a settled turn still waits for. */
export function taskActivity(session: Session | undefined, missing: boolean): string | undefined {
  if (missing) return "This task is no longer available";
  if (!session) return "Loading…";
  if (session.status === "working" && session.subagents) return `${session.subagents} background ${session.subagents === 1 ? "agent" : "agents"} running`;
  if (session.status === "working") return session.activity || undefined;
  if (session.status === "needs-you") return session.activity || TASK_STATUS_LABEL[session.status];
  return session.status === "done" ? undefined : TASK_STATUS_LABEL[session.status];
}

/** The tasks one reply started, one row each (live status, title, activity, harness); clicking a row opens that task beside the conversation. */
export function TaskChips({ parts }: { parts: readonly MsgPart[] }) {
  const ids = parts.map((part) => part.id ?? "");
  const { sessions, missing } = useTaskSessions(ids);
  const openId = useChat((s) => s.pane?.kind === "task" ? s.pane.id : null);
  const rows = parts.map((part, i): SubagentItem => {
    const session = sessions[i];
    const gone = !session && missing[ids[i]!] === true;
    const kind = session?.kind || part.name || "";
    return {
      name: session ? sessionTitle(session) : part.title || "Code task",
      model: kind ? <HarnessBadge kind={kind} /> : "",
      status: gone ? "failed" : session ? ROW_STATUS[session.status] : "running",
      activity: taskActivity(session, gone),
    };
  });
  const selected = ids.indexOf(openId ?? "");
  return <SubagentList aria-label="Code tasks" agents={rows} selectedIndex={selected === -1 ? undefined : selected}
    onOpen={(index) => { const id = ids[index]; if (id) useChat.getState().openTaskPane(id); }} />;
}
