import type { Session } from "@/mygo";
import { ErrorNote } from "@/features/agents/shared";
import { isRemoteProcess } from "@/features/agents/format";
import { useUI } from "@/stores/ui";
import { Composer } from "./Composer";
import { Feed } from "./Feed";
import { TaskCanvas } from "./TaskCanvas";
import { RAIL_WIDTH, SessionRail } from "./rail/SessionRail";
import { useTaskEvents } from "./useTaskEvents";

/**
 * Task conversation. The composer sticks to the bottom of the conversation's scroll viewport and, with `rail` (the Tasks page,
 * not the narrow chat side pane) and the titlebar toggle on, the session rail is a column inside it, so one scrollbar runs the
 * full height at the far right.
 */
export function TaskView({ session, rail = false }: { session: Session; rail?: boolean }) {
  const task = useTaskEvents(session.id);
  const remote = isRemoteProcess(session);
  const railOpen = useUI((state) => rail && state.taskRailOpen);
  return <TaskCanvas session={session}>
    <div className="flex h-full min-h-0 flex-col">
      <div className="px-5"><ErrorNote error={session.error} /></div>
      {remote && <p className="px-5 py-2 text-xs text-muted-foreground">SSH process · output and exit status only. There is no interactive terminal, conversation resume, or local worktree review.</p>}
      <Feed session={session} events={task.events} loading={task.loading} loadingMore={task.loadingMore} error={task.error} hasMore={task.hasMore} onLoadMore={task.loadMore} onRetry={() => void task.reload()}
        aside={railOpen ? { node: <SessionRail session={session} events={task.events} />, width: RAIL_WIDTH } : undefined}
        footer={<>
          <Composer session={session} />
          {remote && <p className="border-t bg-background px-5 py-2 text-[11px] text-muted-foreground">The daemon tracks this process over SSH. An interrupted connection does not confirm that the remote process stopped.</p>}
        </>} />
    </div>
  </TaskCanvas>;
}
