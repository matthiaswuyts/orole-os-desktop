import { AgentGlyph } from "@/features/agents/shared";
import type { Session } from "@/mygo";
import { DeleteTaskButton } from "./DeleteTask";

/** The titlebar of an open task, or of a task that is still starting: the provider's logo and the task's title, and for a started task its delete button. */
export function TaskHeader({ kind, title, session }: { kind: string; title: string; session?: Session }) {
  return <div className="flex min-w-0 flex-1 items-center gap-2">
    <span className="titlebar-nodrag shrink-0"><AgentGlyph kind={kind} /></span>
    <h1 className="min-w-0 truncate text-xs leading-4 font-medium">{title}</h1>
    {session && <span className="titlebar-nodrag ml-auto shrink-0"><DeleteTaskButton session={session} /></span>}
  </div>;
}
