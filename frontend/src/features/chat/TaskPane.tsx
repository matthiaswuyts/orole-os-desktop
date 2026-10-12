import { useEffect, useRef } from "react";
import { SquareArrowOutUpRightIcon, XIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { sessionTitle } from "@/features/agents/format";
import { openTask } from "@/features/projects/tasks";
import { TaskView } from "@/features/tasks/TaskView";
import { useChat } from "@/stores/chat";
import { HarnessBadge, TaskStatusGlyph } from "./parts/TaskChip";
import { TASK_STATUS_LABEL, useTaskSession } from "./taskSession";

/**
 * A code task beside the bot's conversation: the agent's live feed and the task composer, so the user can talk to the
 * coding agent directly. Wide windows split the view; narrow ones lay the pane over the conversation. Mount keyed by task id.
 */
export function TaskPane({ taskId }: { taskId: string }) {
  const { session, missing } = useTaskSession(taskId);
  const heading = useRef<HTMLHeadingElement>(null);
  // The opener is a chip in the thread; it stays put, so focus moves to the pane and back to the thread on close.
  const opener = useRef(document.activeElement instanceof HTMLElement ? document.activeElement : null);
  useEffect(() => { heading.current?.focus(); }, []);
  const close = () => {
    useChat.getState().closeTaskPane();
    opener.current?.focus();
  };
  const title = session ? sessionTitle(session) : "Code task";
  return <section aria-labelledby="task-pane-title" data-slot="task-pane"
    className="absolute inset-y-0 right-0 z-20 flex w-full max-w-[34rem] min-w-0 flex-col border-l bg-background shadow-xl transition-transform duration-200 ease-out starting:translate-x-full motion-reduce:transition-none @3xl/chat:static @3xl/chat:z-auto @3xl/chat:w-[45%] @3xl/chat:max-w-none @3xl/chat:min-w-[24rem] @3xl/chat:shrink-0 @3xl/chat:shadow-none">
    <header className="flex h-10 shrink-0 items-center gap-2 border-b px-3">
      {session && <TaskStatusGlyph status={session.status} />}
      <h2 id="task-pane-title" ref={heading} tabIndex={-1} title={title} className="min-w-0 flex-1 truncate text-sm font-medium outline-none">{title}</h2>
      {session && <span className="sr-only">{TASK_STATUS_LABEL[session.status]}</span>}
      {session?.kind && <HarnessBadge kind={session.kind} />}
      <Button type="button" variant="ghost" size="xs" onClick={() => openTask(taskId)}><SquareArrowOutUpRightIcon aria-hidden />Open in Tasks</Button>
      <Button type="button" variant="ghost" size="icon-xs" aria-label="Close task" onClick={close}><XIcon aria-hidden /></Button>
    </header>
    <div className="flex min-h-0 flex-1 flex-col">
      {session
        ? <TaskView session={session} />
        : <p role="status" className="m-auto p-6 text-center text-sm text-muted-foreground">{missing ? "This task is no longer available." : "Loading the task…"}</p>}
    </div>
  </section>;
}
