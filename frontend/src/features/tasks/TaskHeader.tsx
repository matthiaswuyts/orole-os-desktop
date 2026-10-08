import { useState } from "react";
import { ArrowLeft, GitPullRequest } from "lucide-react";
import { Agents, type Session } from "@/mygo";
import { useAgents } from "@/stores/agents";
import { useBoxes } from "@/stores/boxes";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { AgentGlyph, SessionBadge } from "@/features/agents/shared";
import { duration, folderOf, isRemoteProcess, kindLabel, sessionTitle, useNow } from "@/features/agents/format";
import { toastError } from "@/features/projects/dialogs";
import { openExternal } from "@/lib/openExternal";
import { TaskSheets } from "./TaskSheets";

export function TaskHeader({ session }: { session: Session }) {
  const [stopping, setStopping] = useState(false);
  const machine = useBoxes((state) => state.machines.find((box) => box.id === (session.machine || "local"))?.name);
  const now = useNow();
  const title = sessionTitle(session);
  const metadata = [kindLabel(session.kind), session.model, session.reasoning ? `${session.reasoning} reasoning` : "", machine ?? session.machine, folderOf(session), session.branch, duration(now - Date.parse(session.startedAt))].filter(Boolean).join(" · ");
  const stop = async () => {
    if (stopping) return;
    setStopping(true);
    try { useAgents.getState().upsert(await Agents.stop(session.id)); }
    catch (error) { toastError("Could not stop task", error); }
    finally { setStopping(false); }
  };
  return <div className="flex min-w-0 flex-1 items-center gap-2">
    <Button size="icon-xs" variant="ghost" className="titlebar-nodrag shrink-0" aria-label="Back to tasks" onClick={() => useAgents.getState().setOpenId(null)}><ArrowLeft /></Button>
    <span className="titlebar-nodrag shrink-0"><AgentGlyph kind={session.kind} /></span>
    <Tooltip>
      <TooltipTrigger render={<div tabIndex={0} className="min-w-0 flex-1" />}>
        <h1 className="truncate text-xs leading-4 font-medium">{title}</h1>
        <p className="truncate text-[10px] leading-3 text-muted-foreground">{metadata}</p>
      </TooltipTrigger>
      <TooltipPopup><span className="whitespace-pre-wrap break-words">{`${title}\n${metadata}${session.dir ? `\n${session.dir}` : ""}`}</span></TooltipPopup>
    </Tooltip>
    <div className="titlebar-nodrag flex shrink-0 items-center gap-2">
      <TaskSheets session={session} />
      {session.prUrl && <Button size="xs" variant="ghost" aria-label={`Open pull request ${session.prNumber}`} onClick={() => void openExternal(session.prUrl)}><GitPullRequest data-icon="inline-start" />#{session.prNumber}</Button>}
      <SessionBadge session={session} />
      {session.live && <Button size="xs" variant="outline" disabled={stopping} onClick={() => void stop()}>{isRemoteProcess(session) ? "Request stop" : "Stop"}</Button>}
    </div>
  </div>;
}
