import { ListOrderedIcon } from "lucide-react";
import { Agents, type Session } from "@/mygo";
import { useAgents } from "@/stores/agents";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { useAction } from "@/features/agents/format";

/** The daemon owns order and delivery. Removing an uncertain receipt does not undo a send. */
export function MessageQueue({ session }: { session: Session }) {
  const action = useAction();
  const entries = session.queue ?? [];
  if (entries.length === 0) return null;
  const uncertain = entries.some((entry) => entry.state === "uncertain");
  const sending = entries.some((entry) => entry.state === "sending");
  const paused = entries.some((entry) => entry.state === "paused");
  const canResume = paused && !uncertain && !sending && session.live &&
    ["idle", "done", "failed"].includes(session.status) && !session.pending?.length && !session.questions?.length;

  return <section aria-label="Message queue" className="flex flex-col gap-2 text-xs">
    <div className="flex flex-wrap items-center justify-between gap-2">
      <h3 className="flex items-center gap-2 font-medium"><ListOrderedIcon aria-hidden className="size-4" />{entries.length} {entries.length === 1 ? "follow-up" : "follow-ups"}</h3>
      {paused && <Button type="button" size="xs" variant="outline" disabled={!canResume || action.busy} onClick={() => void action.run(async () => {
        useAgents.getState().upsert(await Agents.resumeQueue(session.id));
      })}>Resume queue</Button>}
    </div>
    {uncertain ? <p role="status" className="text-warning">Delivery is uncertain. Check the conversation before removing this receipt or sending again. The daemon will not retry it automatically.</p>
      : paused ? <p role="status" className="text-muted-foreground">Queue paused. {session.live ? "Resolve any pending approval or question, then resume when the agent is not working." : "Send a message to resume this task first, then resume the queue."}</p>
      : <p className="text-muted-foreground">Follow-ups send in order after the current turn succeeds.</p>}
    <ol className="flex max-h-52 flex-col divide-y overflow-y-auto rounded-lg border">
      {entries.map((entry) => <li key={entry.id} className="flex items-start gap-3 p-3">
        <div className="flex min-w-0 flex-1 flex-col gap-1.5">
          <div className="flex flex-wrap items-center gap-2"><Badge variant={entry.state === "uncertain" ? "warning" : "secondary"}>{entry.state === "uncertain" ? "Delivery uncertain" : entry.state}</Badge><time dateTime={entry.createdAt} className="text-muted-foreground">{new Date(entry.createdAt).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}</time></div>
          <p className="selectable whitespace-pre-wrap break-words">{entry.text}</p>
          {entry.error && <p className="whitespace-pre-wrap break-words text-destructive-foreground">{entry.error}</p>}
        </div>
        <Button type="button" size="xs" variant="ghost" disabled={entry.state === "sending" || action.busy} aria-label={entry.state === "uncertain" ? `Remove uncertain receipt: ${entry.text}` : `Cancel queued message: ${entry.text}`} onClick={() => void action.run(async () => {
          useAgents.getState().upsert(await Agents.cancelQueuedPrompt(session.id, entry.id));
        })}>{entry.state === "uncertain" ? "Remove receipt" : "Cancel"}</Button>
      </li>)}
    </ol>
    {action.error && <p role="alert" className="text-destructive-foreground">{action.error}</p>}
  </section>;
}
