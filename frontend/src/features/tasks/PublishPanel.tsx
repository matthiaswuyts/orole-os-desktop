import { useId, useState } from "react";
import { Check, ChevronRight, Circle, CircleX, ExternalLink, GitBranch, GitCommitHorizontal, GitPullRequest, LoaderCircle, Upload } from "lucide-react";
import { Agents, type Session } from "@/mygo";
import { useAgents } from "@/stores/agents";
import { AlertDialog, AlertDialogClose, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogPopup, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Collapsible, CollapsiblePanel, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { toastError } from "@/features/projects/dialogs";
import { sessionTitle, useAction } from "@/features/agents/format";
import { openExternal } from "@/lib/openExternal";
import { cn } from "@/lib/utils";
import { PublishError, STEP_FAILURE, STEP_LABEL, needsConfirmation, publishBlocker, pullRequestPlan, runPublish, stepsFor, type PublishGoal, type PublishStep, type StepState } from "./publish";

const stateLabel: Record<StepState, string> = { pending: "Waiting", running: "In progress", done: "Done", failed: "Failed" };

function StepIcon({ state }: { state: StepState }) {
  const Icon = state === "running" ? LoaderCircle : state === "done" ? Check : state === "failed" ? CircleX : Circle;
  return <Icon aria-hidden className={cn("size-3.5 shrink-0", state === "running" && "animate-spin text-info motion-reduce:animate-none", state === "done" && "text-success", state === "failed" && "text-destructive", state === "pending" && "text-muted-foreground")} />;
}

/** Commit, push and pull request for a session's branch. Lives in the Changes sheet. */
export function PublishPanel({ session }: { session: Session }) {
  const ids = { message: useId(), title: useId(), base: useId(), body: useId(), draft: useId() };
  const action = useAction();
  const [message, setMessage] = useState("");
  const [title, setTitle] = useState("");
  const [base, setBase] = useState("");
  const [body, setBody] = useState("");
  const [draft, setDraft] = useState(false);
  const [confirming, setConfirming] = useState<PublishGoal | null>(null);
  const [run, setRun] = useState<{ goal: PublishGoal; states: Partial<Record<PublishStep, StepState>> } | null>(null);
  const blocker = publishBlocker(session);
  const disabled = blocker !== null || action.busy;
  const hasPR = Boolean(session.prUrl);
  const form = { message, title, body, base, draft };

  const publish = (goal: PublishGoal, confirmed: boolean) => void action.run(async () => {
    setRun({ goal, states: {} });
    try {
      const latest = await runPublish(session, goal, form, confirmed, Agents.publish, (step, state) => setRun((cur) => cur && { ...cur, states: { ...cur.states, [step]: state } }));
      useAgents.getState().upsert(latest);
    } catch (e) {
      // Earlier steps may have completed (a commit, a push): refresh so the branch state and PR link are current.
      void Agents.get(session.id).then((fresh) => useAgents.getState().upsert(fresh), () => undefined);
      toastError(e instanceof PublishError ? STEP_FAILURE[e.step] : "Publish failed", e);
    }
  });
  const start = (goal: PublishGoal) => { if (needsConfirmation(goal)) setConfirming(goal); else publish(goal, false); };

  const steps = run ? stepsFor(run.goal) : [];
  const pr = pullRequestPlan(session, form);
  return <section aria-label="Publish" className="shrink-0 border-b bg-muted/20 px-4 py-3 text-sm">
    <div className="mb-2 flex flex-wrap items-center gap-2">
      <h3 className="text-xs font-medium">Publish</h3>
      <span className="inline-flex min-w-0 items-center gap-1 text-xs text-muted-foreground"><GitBranch aria-hidden className="size-3.5 shrink-0" /><span className="truncate font-mono">{session.branch || "no branch"}</span></span>
      {hasPR && <Button size="xs" variant="outline" className="ml-auto" onClick={() => void openExternal(session.prUrl)}><GitPullRequest data-icon="inline-start" />Pull request #{session.prNumber}<ExternalLink data-icon="inline-end" /></Button>}
    </div>
    {blocker && <p role="status" className="mb-2 text-xs text-muted-foreground">{blocker}</p>}
    {!blocker && session.status === "working" && <p role="status" className="mb-2 text-xs text-muted-foreground">The agent is still working; publishing now commits what it has written so far.</p>}
    <div className="flex flex-col gap-2">
      <label htmlFor={ids.message} className="text-xs font-medium text-muted-foreground">Commit message</label>
      <Textarea id={ids.message} size="sm" rows={2} disabled={disabled} placeholder="Leave empty to use the task title" value={message} onChange={(e) => setMessage(e.target.value)} />
      <Collapsible>
        <CollapsibleTrigger className="group flex items-center gap-1 rounded text-xs text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"><ChevronRight aria-hidden className="size-3.5 transition-transform group-data-[panel-open]:rotate-90 motion-reduce:transition-none" />Pull request details</CollapsibleTrigger>
        <CollapsiblePanel>
          <div className="flex flex-col gap-2 pt-2">
            <label htmlFor={ids.title} className="text-xs font-medium text-muted-foreground">Title</label>
            <Input id={ids.title} size="sm" disabled={disabled} placeholder={sessionTitle(session)} value={title} onChange={(e) => setTitle(e.target.value)} />
            <label htmlFor={ids.base} className="text-xs font-medium text-muted-foreground">Base branch</label>
            <Input id={ids.base} size="sm" disabled={disabled} placeholder="Default branch of the repository" value={base} onChange={(e) => setBase(e.target.value)} />
            <label htmlFor={ids.body} className="text-xs font-medium text-muted-foreground">Description</label>
            <Textarea id={ids.body} size="sm" rows={3} disabled={disabled} placeholder="Leave empty to use the task and the agent's result" value={body} onChange={(e) => setBody(e.target.value)} />
            <div className="flex items-center gap-2"><Checkbox id={ids.draft} checked={draft} disabled={disabled} onCheckedChange={setDraft} /><label htmlFor={ids.draft} className="text-xs">Open as draft</label></div>
          </div>
        </CollapsiblePanel>
      </Collapsible>
      <div className="flex flex-wrap gap-2">
        <Button size="sm" variant="outline" disabled={disabled} loading={action.busy && run?.goal === "commit"} onClick={() => start("commit")}><GitCommitHorizontal data-icon="inline-start" />Commit</Button>
        <Button size="sm" variant="outline" disabled={disabled} loading={action.busy && run?.goal === "push"} onClick={() => start("push")}><Upload data-icon="inline-start" />Push branch</Button>
        {!hasPR && <Button size="sm" disabled={disabled} loading={action.busy && run?.goal === "pr"} onClick={() => start("pr")}><GitPullRequest data-icon="inline-start" />Open pull request</Button>}
      </div>
      {steps.length > 0 && <ol aria-label="Publish progress" className="flex flex-col gap-1 text-xs">
        {steps.map((step) => { const state = run?.states[step] ?? "pending"; return <li key={step} className="flex items-center gap-2"><StepIcon state={state} /><span>{STEP_LABEL[step]}</span><span className={cn("text-muted-foreground", state === "failed" && "text-destructive-foreground")}>{stateLabel[state]}</span></li>; })}
      </ol>}
    </div>
    <AlertDialog open={confirming !== null} onOpenChange={(open) => { if (!open) setConfirming(null); }}>
      <AlertDialogPopup>
        <AlertDialogHeader>
          <AlertDialogTitle>{confirming === "pr" ? "Push branch and open a pull request?" : "Push branch?"}</AlertDialogTitle>
          <AlertDialogDescription render={<div />}>
            <p>This sends the work off this machine. It will:</p>
            <ul className="mt-2 list-disc space-y-1 pl-5">
              <li>Commit any uncommitted changes in the session worktree{message.trim() ? <> with the message <q className="selectable">{message.trim()}</q></> : " with a message generated from the task title"}.</li>
              <li>Push branch <code className="selectable font-mono">{session.branch}</code> to the <code className="font-mono">origin</code> remote.</li>
              {confirming === "pr" && <li>Open a {pr.draft ? "draft " : ""}pull request titled <q className="selectable">{pr.title}</q> into {pr.base}.</li>}
            </ul>
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogClose render={<Button variant="outline" />}>Cancel</AlertDialogClose>
          <Button onClick={() => { const goal = confirming; setConfirming(null); if (goal) publish(goal, true); }}>{confirming === "pr" ? "Push and open pull request" : "Push branch"}</Button>
        </AlertDialogFooter>
      </AlertDialogPopup>
    </AlertDialog>
  </section>;
}
