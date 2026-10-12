import { History, LoaderCircle } from "lucide-react";
import { useShallow } from "zustand/react/shallow";
import { Widget } from "@/components/widgets/Widget";
import type { WidgetProps } from "@/components/widgets/registry";
import { onDashboard } from "@/features/agents/columns";
import { ago, folderOf, sessionDuration, sessionTitle, useNow } from "@/features/agents/format";
import { ProviderLogo } from "@/features/agents/ProviderLogo";
import { openTask } from "@/features/projects/tasks";
import { cn } from "@/lib/utils";
import type { Session } from "@/mygo";
import { useAgents } from "@/stores/agents";
import { useTasksStatus } from "./useTasksStatus";

const STATUS: Record<string, { label: string; dot: string }> = {
  working: { label: "Working", dot: "bg-info animate-pulse motion-reduce:animate-none" },
  "needs-you": { label: "Needs you", dot: "bg-warning" },
  failed: { label: "Failed", dot: "bg-destructive" },
  done: { label: "Done", dot: "bg-success" },
  idle: { label: "Idle", dot: "bg-muted-foreground/50" },
  stopped: { label: "Stopped", dot: "bg-muted-foreground/50" },
  lost: { label: "Lost", dot: "bg-muted-foreground/50" },
};

function TaskRow({ session: s, detail, aside }: { session: Session; detail: string; aside: string }) {
  const status = STATUS[s.status] ?? STATUS.idle!;
  return (
    <li>
      <button type="button" onClick={() => openTask(s.id)}
        className="flex w-full items-center gap-3 rounded-md px-2 py-1.5 text-left outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring">
        <span className="relative inline-flex size-5 shrink-0 items-center justify-center [&_svg]:size-4" aria-hidden><ProviderLogo kind={s.kind} /></span>
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="truncate text-[13px]">{sessionTitle(s)}</span>
          <span className="truncate text-muted-foreground text-xs">{detail}</span>
        </span>
        <span className="flex shrink-0 items-center gap-1.5 text-muted-foreground text-xs">
          <span className={cn("size-1.5 rounded-full", status.dot)} aria-hidden />
          <span className="sr-only">{status.label}, </span>{aside}
        </span>
      </button>
    </li>
  );
}

/** The latest tasks by last activity; a row opens the task. */
export function RecentTasksWidget({ onRemove }: WidgetProps) {
  const { status, message } = useTasksStatus();
  const recent = useAgents(useShallow((s) => Object.values(s.sessions).filter(onDashboard)
    .map((session) => ({ session, at: Date.parse(session.updatedAt) || 0 })).sort((a, b) => b.at - a.at).slice(0, 8).map((entry) => entry.session)));
  const now = useNow(recent.length > 0);
  return (
    <Widget title="Recent tasks" icon={<History />} status={status} message={message} onRemove={onRemove}>
      {recent.length === 0
        ? <p className="flex h-full min-h-28 items-center justify-center text-center text-muted-foreground text-xs">No tasks yet. Describe one above to get started.</p>
        : <ul className="-m-1.5 flex flex-col">
            {recent.map((s) => <TaskRow key={s.id} session={s} detail={[folderOf(s), STATUS[s.status]?.label ?? s.status].filter(Boolean).join(" · ")} aside={ago(s.updatedAt, now)} />)}
          </ul>}
    </Widget>
  );
}

/** Agents working right now, with what each is doing and for how long. */
export function RunningNowWidget({ onRemove }: WidgetProps) {
  const { status, message } = useTasksStatus();
  const running = useAgents(useShallow((s) => Object.values(s.sessions).filter((session) => session.status === "working" && onDashboard(session))
    .sort((a, b) => Date.parse(a.startedAt) - Date.parse(b.startedAt))));
  const now = useNow(running.length > 0);
  return (
    <Widget title="Running now" icon={<LoaderCircle />} status={status} message={message} onRemove={onRemove}
      count={running.length > 0 ? <span className="text-muted-foreground text-xs tabular-nums">{running.length}</span> : undefined}>
      {running.length === 0
        ? <p className="flex h-full min-h-28 items-center justify-center text-center text-muted-foreground text-xs">No agent is working right now.</p>
        : <ul className="-m-1.5 flex flex-col">
            {running.map((s) => <TaskRow key={s.id} session={s} detail={s.activity || folderOf(s)} aside={sessionDuration(s, now)} />)}
          </ul>}
    </Widget>
  );
}
