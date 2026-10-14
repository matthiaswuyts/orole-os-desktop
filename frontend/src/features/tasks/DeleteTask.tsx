import { useState } from "react";
import { Trash2Icon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { sessionTitle } from "@/features/agents/format";
import { ConfirmDialog } from "@/features/projects/dialogs";
import { Agents, type Session } from "@/mygo";
import { useAgents } from "@/stores/agents";
import { useChat } from "@/stores/chat";

/**
 * Deletes a task. The daemon only removes tasks that are not running, so a running one is stopped first. Afterwards the
 * task leaves the list and, if it was open on the Tasks page or beside a bot's chat, that view closes.
 */
export async function deleteTask(session: Pick<Session, "id" | "live">) {
  if (session.live) await Agents.stop(session.id);
  await Agents.remove(session.id);
  useAgents.getState().drop(session.id);
  const pane = useChat.getState().pane;
  if (pane?.kind === "task" && pane.id === session.id) useChat.getState().closePane();
}

/** Confirmation for deleting a task. Branches and worktrees stay on disk: the daemon never removes them. */
export function DeleteTaskDialog({ session, open, onOpenChange }: { session: Session; open: boolean; onOpenChange: (open: boolean) => void }) {
  return (
    <ConfirmDialog open={open} onOpenChange={onOpenChange} title={`Delete ${sessionTitle(session)}?`} confirmLabel="Delete task" onConfirm={() => deleteTask(session)}>
      <p>{session.live ? "This task is running. It will be stopped, then deleted. " : ""}The task and its history (conversation and scratch files) are permanently removed. Files and branches in your project stay on disk.</p>
    </ConfirmDialog>
  );
}

/** Ghost icon button that asks before deleting the task. */
export function DeleteTaskButton({ session }: { session: Session }) {
  const [open, setOpen] = useState(false);
  return <>
    <Tooltip>
      <TooltipTrigger render={<Button type="button" variant="ghost" size="icon-xs" aria-label="Delete task" className="titlebar-nodrag" onClick={() => setOpen(true)} />}>
        <Trash2Icon aria-hidden />
      </TooltipTrigger>
      <TooltipPopup side="bottom">{session.live ? "Stop and delete task" : "Delete task"}</TooltipPopup>
    </Tooltip>
    {open && <DeleteTaskDialog session={session} open onOpenChange={setOpen} />}
  </>;
}
