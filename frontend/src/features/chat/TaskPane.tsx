import { ChevronLeftIcon, SquareArrowOutUpRightIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { sessionTitle } from "@/features/agents/format";
import { openTask } from "@/features/projects/tasks";
import { TaskView } from "@/features/tasks/TaskView";
import { useTaskSession } from "./taskSession";

/**
 * A code task beside the bot's conversation: the agent's live feed and the task composer, so the user can talk to the
 * coding agent directly. The side pane hosts it; until the pane has finished opening (`ready`) only the header and a
 * skeleton draw, so the slide never competes with the feed's first paint.
 */
export function TaskPane({ taskId, ready, onClose }: { taskId: string; ready: boolean; onClose: () => void }) {
  const { session, missing } = useTaskSession(taskId);
  const title = session ? sessionTitle(session) : "Code task";
  return <section aria-labelledby="side-pane-title" data-slot="task-pane" className="flex h-full min-w-0 flex-col">
    <header className="flex h-10 shrink-0 items-center gap-2 border-b px-3">
      <Button type="button" variant="ghost" size="icon-xs" aria-label="Close task pane" onClick={onClose}><ChevronLeftIcon aria-hidden /></Button>
      <h2 id="side-pane-title" tabIndex={-1} className="min-w-0 flex-1 truncate text-sm font-medium outline-none">{title}</h2>
      <Tooltip>
        <TooltipTrigger render={<Button type="button" variant="ghost" size="icon-xs" aria-label="Open task page" onClick={() => openTask(taskId)} />}>
          <SquareArrowOutUpRightIcon aria-hidden />
        </TooltipTrigger>
        <TooltipPopup>Open task page</TooltipPopup>
      </Tooltip>
    </header>
    <div className="flex min-h-0 flex-1 flex-col">
      {session && ready
        ? <TaskView session={session} />
        : session
          ? <div aria-hidden className="flex flex-col gap-2 p-3"><Skeleton className="h-4 w-3/4" /><Skeleton className="h-4 w-1/2" /><Skeleton className="h-4 w-2/3" /></div>
          : <p role="status" className="m-auto p-6 text-center text-sm text-muted-foreground">{missing ? "This task is no longer available." : "Loading the task…"}</p>}
    </div>
  </section>;
}
