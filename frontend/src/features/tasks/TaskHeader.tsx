import { AgentGlyph } from "@/features/agents/shared";

/** The titlebar of an open task, or of a task that is still starting: the provider's logo and the task's title. */
export function TaskHeader({ kind, title }: { kind: string; title: string }) {
  return <div className="flex min-w-0 flex-1 items-center gap-2">
    <span className="titlebar-nodrag shrink-0"><AgentGlyph kind={kind} /></span>
    <h1 className="min-w-0 truncate text-xs leading-4 font-medium">{title}</h1>
  </div>;
}
