import type { Session } from "@/mygo";
import { AgentGlyph } from "@/features/agents/shared";
import { sessionTitle } from "@/features/agents/format";

export function TaskHeader({ session }: { session: Session }) {
  const title = sessionTitle(session);
  return <div className="flex min-w-0 flex-1 items-center gap-2">
    <span className="titlebar-nodrag shrink-0"><AgentGlyph kind={session.kind} /></span>
    <h1 className="min-w-0 truncate text-xs leading-4 font-medium" title={title}>{title}</h1>
  </div>;
}
