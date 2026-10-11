import { ComposerAttachmentChip, ComposerAttachments } from "@/components/assistant-ui/elements/composer-attachments";
import { WorkingStatus } from "@/components/assistant-ui/elements/working-status";
import { Button } from "@/components/ui/button";
import { kindLabel } from "@/features/agents/format";
import { ErrorNote } from "@/features/agents/shared";
import { UserMessage } from "./FeedItems";
import { editLaunch, retryLaunch, type PendingLaunch } from "./pendingLaunch";

/**
 * The task page of a task that was just sent, before the daemon has returned its session: what the real page will show first
 * (the person's message, the agent starting), with no output of its own. It uses the real page's layout containers so the swap
 * to `TaskView` leaves the message where it was. A failed start stays here with its error, "Try again" and "Edit prompt".
 */
export function PendingTaskView({ launch }: { launch: PendingLaunch }) {
  const provider = kindLabel(launch.kind);
  const failed = launch.status === "failed";
  return <div className="@container/canvas flex min-h-0 min-w-0 flex-1 flex-col" data-slot="canvas-split" data-testid="pending-task">
    <div className="flex min-h-0 min-w-0 flex-1 flex-col">
      <div className="flex h-full min-h-0 flex-col">
        <div className="relative flex min-h-0 flex-1 flex-col">
          <div role="log" aria-label="Task conversation" aria-live="off" className="min-h-0 flex-1 overflow-auto px-5 py-4 pb-14">
            <div className="mx-auto flex max-w-4xl flex-col gap-4">
              {launch.prompt && <UserMessage item={{ type: "user", key: launch.id, at: new Date(launch.startedAt).toISOString(), text: launch.prompt }} />}
              {launch.attachments.length > 0 && <ComposerAttachments aria-label="Attachments" className="justify-end">
                {launch.attachments.map((file) => <ComposerAttachmentChip key={file.key} attachment={file} />)}
              </ComposerAttachments>}
              {failed
                ? <div className="flex flex-col items-start gap-3" data-testid="pending-failure">
                  <p className="text-sm font-medium">Could not start the task</p>
                  <ErrorNote error={launch.error || "The agent did not start."} />
                  <div className="flex gap-2">
                    <Button size="sm" onClick={() => retryLaunch(launch.id)}>Try again</Button>
                    <Button size="sm" variant="outline" onClick={() => editLaunch(launch.id)}>Edit prompt</Button>
                  </div>
                </div>
                : <WorkingStatus label={launch.worktree ? "Preparing workspace" : `Starting ${provider}`} since={launch.startedAt} />}
            </div>
          </div>
        </div>
        <div className="bg-background px-5 pt-2 pb-3">
          <div className="mx-auto flex max-w-4xl flex-col gap-1">
            <div className="rounded-lg border bg-card px-4 py-3 text-muted-foreground text-sm">
              <textarea aria-label="Message the agent" disabled rows={1} placeholder={failed ? "The task has not started" : `Available once ${provider} has started`} className="w-full resize-none bg-transparent outline-none placeholder:text-muted-foreground" />
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>;
}
