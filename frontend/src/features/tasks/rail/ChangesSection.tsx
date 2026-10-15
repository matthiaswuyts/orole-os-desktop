import { useState, type FormEvent } from "react";
import { FileDiffIcon, GitPullRequestIcon } from "lucide-react";
import { Agents, type Session } from "@/mygo";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogDescription, DialogFooter, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { openExternal } from "@/lib/openExternal";
import { useAgents } from "@/stores/agents";
import { sessionTitle, useAction } from "@/features/agents/format";
import { RailSection } from "./RailSection";

const count = new Intl.NumberFormat("en");

/** Why Create PR cannot run for this task right now, or null when it can. Go checks the same conditions again. */
function publishBlocked(session: Session): string | null {
  if (session.machine && session.machine !== "local") return "Pull requests can only be created for tasks on this computer.";
  if (session.live && (session.status === "working" || session.status === "needs-you")) return "The agent is still working. Wait for its turn to end, or stop it.";
  if (!session.files && !session.additions && !session.deletions) return "There are no changes to publish yet.";
  return null;
}

/** The task's diff against its base (lines added and removed, files), and Create PR, or the pull request once there is one. */
export function ChangesSection({ session }: { session: Session }) {
  const [creating, setCreating] = useState(false);
  const empty = !session.files && !session.additions && !session.deletions;
  const blocked = publishBlocked(session);
  const create = <Button size="xs" variant="secondary" disabled={blocked !== null} onClick={() => setCreating(true)}>Create PR</Button>;
  return <RailSection id="changes" title="Changes" summary={empty ? "No changes" : `+${count.format(session.additions)} −${count.format(session.deletions)}`}>
    <div className="flex min-h-7 min-w-0 items-center gap-2 text-[13px] tabular-nums">
      <FileDiffIcon aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
      {empty ? <span className="text-muted-foreground">No changes yet</span> : <>
        <span className="text-success-foreground">+{count.format(session.additions)}</span>
        <span className="text-destructive-foreground">−{count.format(session.deletions)}</span>
        <span className="truncate text-xs text-muted-foreground">{session.files} {session.files === 1 ? "file" : "files"}</span>
      </>}
      <span className="ml-auto shrink-0">
        {session.prUrl
          ? <Button size="xs" variant="secondary" onClick={() => void openExternal(session.prUrl)}><GitPullRequestIcon />PR #{session.prNumber}</Button>
          : blocked ? <Tooltip><TooltipTrigger render={<span tabIndex={0} className="inline-flex" />}>{create}</TooltipTrigger><TooltipPopup className="max-w-60">{blocked}</TooltipPopup></Tooltip> : create}
      </span>
    </div>
    {creating && <CreatePullRequestDialog session={session} onClose={() => setCreating(false)} />}
  </RailSection>;
}

/**
 * Publishes the task's branch as a pull request through `Agents.Publish`: commits what is uncommitted, pushes the branch to
 * origin and opens the pull request on GitHub. Submitting the dialog is the person's confirmation of those remote effects;
 * Go still applies the GitHub policy gates.
 */
function CreatePullRequestDialog({ session, onClose }: { session: Session; onClose: () => void }) {
  const [title, setTitle] = useState(() => sessionTitle(session));
  const [body, setBody] = useState("");
  const [base, setBase] = useState("");
  const [message, setMessage] = useState("");
  const [draft, setDraft] = useState(false);
  const action = useAction();
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    const published = await action.run(() => Agents.publish(session.id, { message, push: true, pr: true, title, body, base, draft, confirmed: true }));
    if (!published) return;
    useAgents.getState().upsert(published);
    onClose();
  };
  return <Dialog open onOpenChange={(open) => { if (!open && !action.busy) onClose(); }}>
    <DialogPopup>
      <form onSubmit={(event) => void submit(event)} className="contents">
        <DialogHeader>
          <DialogTitle>Create pull request</DialogTitle>
          <DialogDescription>
            Commits the task's uncommitted changes, pushes {session.branch ? <code className="font-mono">{session.branch}</code> : "its branch"} to origin and opens a pull request on GitHub. Everyone with access to the repository can see it.
          </DialogDescription>
        </DialogHeader>
        <DialogPanel className="flex flex-col gap-3">
          <Field>
            <FieldLabel>Title</FieldLabel>
            <Input autoFocus value={title} maxLength={256} onChange={(event) => setTitle(event.target.value)} />
          </Field>
          <Field>
            <FieldLabel>Description</FieldLabel>
            <Textarea value={body} rows={4} placeholder="Leave empty to use the task's prompt and result" onChange={(event) => setBody(event.target.value)} />
          </Field>
          <Field>
            <FieldLabel>Base branch</FieldLabel>
            <Input value={base} placeholder="The branch the task started from" onChange={(event) => setBase(event.target.value)} />
          </Field>
          <Field>
            <FieldLabel>Commit message</FieldLabel>
            <Input value={message} placeholder="Generated from the task title" onChange={(event) => setMessage(event.target.value)} />
            <FieldDescription>Used only when there are uncommitted changes.</FieldDescription>
          </Field>
          <label className="flex items-center gap-2 text-sm"><Checkbox checked={draft} onCheckedChange={(checked) => setDraft(checked === true)} />Open as a draft</label>
          {action.error && <p role="alert" className="text-sm break-words text-destructive-foreground">{action.error}</p>}
        </DialogPanel>
        <DialogFooter>
          <Button type="button" variant="outline" disabled={action.busy} onClick={onClose}>Cancel</Button>
          <Button type="submit" loading={action.busy}>Create pull request</Button>
        </DialogFooter>
      </form>
    </DialogPopup>
  </Dialog>;
}
