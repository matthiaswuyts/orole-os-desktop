import type { Session } from "@/mygo";
import { ErrorNote } from "@/features/agents/shared";
import { isRemoteProcess } from "@/features/agents/format";
import { Composer } from "./Composer";
import { Feed } from "./Feed";
import { TaskCanvas } from "./TaskCanvas";
import { useTaskEvents } from "./useTaskEvents";

/** Task conversation; supporting changes and terminal surfaces live in titlebar sheets. */
export function TaskView({ session }: { session: Session }) {
  const task = useTaskEvents(session.id);
  const remote = isRemoteProcess(session);
  return <TaskCanvas session={session} events={task.events}>
    <div className="flex h-full min-h-0 flex-col">
      <div className="px-5"><ErrorNote error={session.error} /></div>
      {remote && <p className="px-5 py-2 text-xs text-muted-foreground">SSH process · output and exit status only. There is no interactive terminal, conversation resume, or local worktree review.</p>}
      <Feed session={session} events={task.events} loading={task.loading} loadingMore={task.loadingMore} error={task.error} hasMore={task.hasMore} onLoadMore={task.loadMore} onRetry={() => void task.reload()} />
      <Composer session={session} />
      {remote && <p className="shrink-0 border-t px-5 py-2 text-[11px] text-muted-foreground">The daemon tracks this process over SSH. An interrupted connection does not confirm that the remote process stopped.</p>}
    </div>
  </TaskCanvas>;
}
