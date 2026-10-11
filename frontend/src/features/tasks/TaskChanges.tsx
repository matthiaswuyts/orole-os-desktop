import type { Session } from "@/mygo";
import { SessionDiff } from "@/features/agents/shared";
import { PublishPanel } from "./PublishPanel";

/** Body of the Changes sheet; loaded on demand so diff rendering stays out of the entry chunk. */
export function TaskChanges({ session }: { session: Session }) {
  return <>
    <PublishPanel session={session} />
    <div className="flex min-h-64 flex-1 p-3"><SessionDiff session={session} /></div>
  </>;
}
