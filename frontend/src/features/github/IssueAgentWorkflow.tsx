import { useEffect, useId, useMemo, useRef, useState } from "react";
import { useGitHub } from "./store";
import { Bot, ExternalLink, RefreshCw } from "lucide-react";
import { Agents, GitHub, type Issue, type IssueAgentRun, type IssueAgentStart, type IssuePRPreparation } from "@/mygo";
import { useAgents } from "@/stores/agents";
import { useUI } from "@/stores/ui";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogDescription, DialogFooter, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Kbd } from "@/components/ui/kbd";
import { RadioGroup, RadioPrimitive } from "@/components/ui/radio-group";
import { Select, SelectGroup, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetDescription, SheetFooter, SheetHeader, SheetPanel, SheetPopup, SheetTitle } from "@/components/ui/sheet";
import { Textarea } from "@/components/ui/textarea";
import { ProviderLogo } from "@/features/agents/ProviderLogo";
import { ErrorNote, SessionBadge, SessionDiff } from "@/features/agents/shared";
import { kindLabel, newKey, useAction } from "@/features/agents/format";
import { EmptyLine, ErrorBox, Loading, RefreshButton, useAsync, type Async } from "./shared";

export const attemptKey = (repo: string, number: number) => `orole:issue-agent:${repo}#${number}`;

/** Only the pending user-authored intent is cached here; run history and source snapshots live in the daemon. */
export function readAttempt(repo: string, number: number): IssueAgentStart | null {
  const raw = localStorage.getItem(attemptKey(repo, number));
  if (!raw) return null;
  const value: unknown = JSON.parse(raw);
  if (!value || typeof value !== "object") throw new Error("The saved launch request could not be read.");
  const s = value as Partial<IssueAgentStart>;
  if (s.repo !== repo || s.number !== number || s.confirmed !== true ||
      ![s.key, s.projectId, s.kind, s.task, s.issueUpdatedAt].every((v) => typeof v === "string" && v.length > 0)) {
    throw new Error("The saved launch request is incomplete. Clear this site's saved request only after checking agent history.");
  }
  return s as IssueAgentStart;
}

function retrySpec(run: IssueAgentRun): IssueAgentStart {
  return { repo: run.repo, number: run.number, projectId: run.projectId, kind: run.kind, task: run.task, issueUpdatedAt: run.issueUpdatedAt, key: run.key, confirmed: true };
}

export interface IssueAgent {
  history: Async<IssueAgentRun[]>;
  /** A launch request saved on this device that has not been confirmed as started. */
  saved: Async<IssueAgentStart | null>;
  launch: IssueAgentStart | "new" | null;
  setLaunch: (launch: IssueAgentStart | "new" | null) => void;
  setSelected: (run: IssueAgentRun) => void;
  /** The run whose review is shown: the chosen one, else the newest. */
  run: IssueAgentRun | undefined;
  reload: () => void;
  canStart: boolean;
  start: () => void;
}

/** Durable run history, the saved retry (if any) and launch state for one issue. */
export function useIssueAgent(repo: string, issue: Issue): IssueAgent {
  const history = useAsync(() => GitHub.issueAgentRuns(repo, issue.number), [repo, issue.number]);
  const saved = useAsync(async () => readAttempt(repo, issue.number), [repo, issue.number]);
  const [launch, setLaunch] = useState<IssueAgentStart | "new" | null>(null);
  const [selected, setSelected] = useState<IssueAgentRun | null>(null);
  const init = useAgents((s) => s.init);
  useEffect(() => init(), [init]);
  const run = history.data?.find((r) => r.id === selected?.id) ?? selected ?? history.data?.[0];
  useEffect(() => {
    const runs = history.data;
    if (runs?.length) setSelected((current) => runs.find((r) => r.id === current?.id) ?? current ?? runs[0]!);
  }, [history.data]);
  const reload = () => { history.reload(); saved.reload(); };
  const canStart = !saved.loading && !saved.error && !issue.pull_request;
  return { history, saved, launch, setLaunch, setSelected, run, reload, canStart, start: () => { if (canStart) setLaunch(saved.data ?? "new"); } };
}

/** Saved-retry notices, run history and the selected run's review. `quiet` renders nothing for an issue with no runs. */
export function IssueAgentRuns({ agent, quiet = false }: { agent: IssueAgent; quiet?: boolean }) {
  const { history, saved, run, setSelected, setLaunch } = agent;
  const empty = history.data?.length === 0 && !saved.data && !saved.error && !history.error;
  if (quiet && (empty || (history.loading && !history.data && !saved.data))) return null;
  return <section aria-label="Agent runs" className="flex min-w-0 flex-col gap-3">
    {saved.error && <ErrorBox error={saved.error} onRetry={saved.reload} />}
    {saved.data && <p role="status" className="text-xs text-muted-foreground">A launch request is saved on this device. Retry it with the same key to recover its result without starting a duplicate session.</p>}
    {history.error && <ErrorBox error={history.error} onRetry={history.reload} />}
    {history.loading && !history.data && <Loading what="issue agent history" />}
    {!quiet && history.data?.length === 0 && <EmptyLine>No agent runs for this issue yet.</EmptyLine>}
    {!!history.data?.length && <ul aria-label="Durable issue run history" className="flex flex-col gap-1">
      {history.data.map((r) => <li key={r.id}>
        <button type="button" aria-current={r.id === run?.id ? "true" : undefined} onClick={() => setSelected(r)} className="flex w-full flex-wrap items-center gap-2 rounded-md px-2 py-2 text-left text-xs outline-none hover:bg-accent/50 aria-current:bg-accent focus-visible:ring-2 focus-visible:ring-ring">
          <span className="min-w-0 flex-1 break-words">{kindLabel(r.kind)} · {r.branch || "No worktree yet"}</span>
          <Badge variant={r.state === "failed" ? "error" : r.state === "published" ? "success" : "secondary"}>{r.state}</Badge>
          <span className="font-mono text-muted-foreground">{r.id.slice(0, 8)}</span>
        </button>
      </li>)}
    </ul>}
    {history.error && run && <p className="text-xs text-muted-foreground">Run details below are from the last successful history read. Refresh history to reconcile any changes.</p>}
    {run && <IssueRunReview key={run.id} run={run} onChanged={history.reload} onRetry={() => setLaunch(retrySpec(run))} />}
  </section>;
}

/** The launch sheet; renders nothing until `agent.launch` is set. */
export function IssueAgentLauncher({ agent, repo, issue }: { agent: IssueAgent; repo: string; issue: Issue }) {
  const { launch, setLaunch, saved, reload, setSelected } = agent;
  if (!launch) return null;
  return <LaunchIssueAgent repo={repo} number={issue.number} initial={launch === "new" ? null : launch} onClose={() => { setLaunch(null); saved.reload(); }} onChanged={reload} onStarted={(r) => { setSelected(r); setLaunch(null); reload(); }} />;
}

export function IssueAgentWorkflow({ repo, issue }: { repo: string; issue: Issue }) {
  const agent = useIssueAgent(repo, issue);
  const { history, saved } = agent;
  return <section aria-label="Issue agent workflow" className="mt-5 flex min-w-0 flex-col gap-3 border-y py-4">
    <header className="flex flex-wrap items-center gap-2">
      <h3 className="flex-1 font-medium text-sm">Work on this issue</h3>
      <RefreshButton a={history} />
      <Button size="sm" disabled={!agent.canStart} onClick={agent.start}>
        {saved.data ? "Review saved launch…" : "Assign to agent…"}
      </Button>
    </header>
    <p className="text-xs text-muted-foreground">Review the source and write an explicit task. An installed agent runs in a new manager-owned worktree of a matching local project. Nothing is published automatically.</p>
    <IssueAgentRuns agent={agent} />
    <IssueAgentLauncher agent={agent} repo={repo} issue={issue} />
  </section>;
}

const LAST_AGENT_KEY = "orole.task.harness";

function LaunchIssueAgent({ repo, number, initial, onClose, onChanged, onStarted }: {
  repo: string; number: number; initial: IssueAgentStart | null; onClose: () => void; onChanged: () => void; onStarted: (run: IssueAgentRun) => void;
}) {
  const id = useId();
  const source = useAsync(() => GitHub.issue(repo, number), [repo, number]);
  const projects = useAsync(() => GitHub.issueAgentProjects(repo), [repo]);
  const agents = useAsync(() => Agents.detect(false), []);
  const [pending, setPending] = useState(initial);
  const [projectChoice, setProjectChoice] = useState(initial?.projectId ?? "");
  const [kindChoice, setKindChoice] = useState(initial?.kind ?? "");
  const [task, setTask] = useState(initial?.task ?? "");
  const edited = useRef(!!initial);
  const [reviewed, setReviewed] = useState(false);
  const [reviewedSource, setReviewedSource] = useState<string | null>(null);
  const [forget, setForget] = useState(false);
  const action = useAction();
  const installed = useMemo(() => agents.data?.filter((a) => a.installed && a.kind !== "custom") ?? [], [agents.data]);
  const usable = projects.data?.filter((p) => !p.missing) ?? [];
  // Choices are made for the user when there is only one sensible answer; the review checkbox still gates launch.
  const projectId = projectChoice || (usable.length === 1 ? usable[0]!.id : "");
  const kind = kindChoice || (installed.find((a) => a.kind === localStorage.getItem(LAST_AGENT_KEY)) ?? installed[0])?.kind || "";
  const project = projects.data?.find((p) => p.id === projectId);
  const agent = installed.find((a) => a.kind === kind);
  const issue = source.data?.issue;
  const changed = !!pending && !!issue && issue.updated_at !== pending.issueUpdatedAt;
  const ready = !!issue && !!project && !project.missing && !!agent && !!task.trim() && reviewed && reviewedSource === issue.updated_at && !source.loading && !projects.loading && !agents.loading && !source.error && !projects.error && !agents.error;

  useEffect(() => {
    // The title is untrusted GitHub text, so the suggested task refers to the issue instead of quoting it.
    if (!edited.current) setTask(`Resolve ${repo}#${number}. Use the issue as reference material, add or update tests where they fit, and leave the changes uncommitted for my review.`);
  }, [repo, number]);

  const submit = () => {
    if (!ready || !issue) return;
    void action.run(async () => {
      // Persist before dispatch: a lost bridge response or GUI restart must reuse this exact intent.
      const spec = pending ?? { repo, number, projectId, kind, task: task.trim(), issueUpdatedAt: issue.updated_at, key: newKey(), confirmed: true };
      const existing = readAttempt(repo, number);
      if (existing && existing.key !== spec.key) throw new Error("Another launch request is saved for this issue. Close this dialog and review that saved request first.");
      localStorage.setItem(attemptKey(repo, number), JSON.stringify(spec));
      setPending(spec);
      try {
        const result = await GitHub.startIssueAgent(spec);
        localStorage.setItem(LAST_AGENT_KEY, spec.kind);
        if (readAttempt(repo, number)?.key === spec.key) localStorage.removeItem(attemptKey(repo, number));
        onStarted(result);
      } finally {
        onChanged();
      }
    });
  };

  return <Sheet open onOpenChange={(open) => { if (!open && !action.busy) onClose(); }}>
    <SheetPopup className="max-w-lg" showCloseButton={!action.busy}>
      <form className="contents" onSubmit={(e) => { e.preventDefault(); submit(); }}>
        <SheetHeader>
          <SheetTitle className="flex items-center gap-2 text-base"><Bot aria-hidden className="size-4 text-muted-foreground" />{forget ? "Forget this saved retry?" : pending ? `Retry the agent on #${number}` : `Start an agent on #${number}`}</SheetTitle>
          <SheetDescription className="break-words">{issue?.title ?? `${repo}#${number}`}</SheetDescription>
        </SheetHeader>
        <SheetPanel className="flex flex-col gap-4">
          {forget ? <p className="text-sm">This only removes the retry saved on this device. It does not cancel a request, stop an agent, or remove daemon history. Check the run history before starting a separate request.</p> : <>
            {source.loading && <Loading what="current issue source" />}
            {source.error && <ErrorBox error={source.error} onRetry={() => { setReviewed(false); source.reload(); }} />}
            {changed && <p role="alert" className="text-xs text-warning-foreground">The issue changed since this request was reviewed. Retrying can recover an existing session, but the daemon will refuse to launch a new session from this stale source. To use the current source, forget this retry, check history, then review a new request.</p>}
            {pending && <p className="break-all text-xs text-muted-foreground">Retry key: <code>{pending.key}</code>. Task, project, agent and reviewed source version are locked to the original request.</p>}
            <Field>
              <FieldLabel htmlFor={`${id}-project`}>Project</FieldLabel>
              <Select value={projectId} disabled={!!pending || action.busy} onValueChange={(v) => { setProjectChoice(v ?? ""); setReviewed(false); }}>
                <SelectTrigger id={`${id}-project`} className="w-full"><SelectValue>{project?.name ?? (projectId || "Choose a local project")}</SelectValue></SelectTrigger>
                <SelectPopup><SelectGroup>{projects.data?.map((p) => <SelectItem key={p.id} value={p.id} disabled={p.missing}>{p.name} · {p.path}{p.missing ? " (missing)" : ""}</SelectItem>)}</SelectGroup></SelectPopup>
              </Select>
              <FieldDescription>{project ? `${project.path} · ${project.remoteUrl}` : `Only registered local projects whose GitHub origin matches ${repo} are eligible.`}</FieldDescription>
              {projects.error && <ErrorBox error={projects.error} onRetry={projects.reload} />}
              {projects.data?.length === 0 && <p className="text-xs text-muted-foreground">No matching project. Register a local clone in Projects, then <button type="button" className="underline underline-offset-2" onClick={() => { setReviewed(false); projects.reload(); }}>refresh this list</button>.</p>}
            </Field>
            <Field>
              <FieldLabel id={`${id}-agent`}>Agent</FieldLabel>
              <RadioGroup aria-labelledby={`${id}-agent`} value={kind} disabled={!!pending || action.busy} onValueChange={(v) => { setKindChoice(String(v)); setReviewed(false); }} className="flex w-full flex-row flex-wrap gap-2">
                {installed.map((a) => <RadioPrimitive.Root key={a.kind} value={a.kind} aria-labelledby={`${id}-agent-${a.kind}`} className={cn("flex min-w-0 items-center gap-2 rounded-lg border px-3 py-1.5 text-sm outline-none transition-colors hover:bg-accent/50 focus-visible:ring-2 focus-visible:ring-ring data-checked:border-primary data-checked:bg-accent data-disabled:cursor-not-allowed data-disabled:opacity-60")}>
                  <ProviderLogo kind={a.kind} /><span id={`${id}-agent-${a.kind}`} className="truncate">{a.name}</span>{a.version && <span className="truncate font-mono text-[11px] text-muted-foreground">{a.version}</span>}
                </RadioPrimitive.Root>)}
              </RadioGroup>
              {agents.loading && <p role="status" className="text-xs text-muted-foreground">Detecting installed agents…</p>}
              {agents.error && <ErrorBox error={agents.error} onRetry={agents.reload} />}
              {agents.data && installed.length === 0 && <p className="text-xs text-muted-foreground">No supported agent is installed. Install a supported CLI, then <button type="button" className="underline underline-offset-2" disabled={action.busy} onClick={() => void action.run(async () => { await Agents.detect(true); setReviewed(false); agents.reload(); })}>refresh detection</button>.</p>}
              <FieldDescription>The daemon creates and owns a separate worktree for this issue; your checkout is not touched.</FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor={`${id}-task`}>First prompt</FieldLabel>
              <Textarea id={`${id}-task`} required readOnly={!!pending} disabled={action.busy} value={task} rows={6} className="[&_textarea]:font-mono [&_textarea]:text-sm"
                onChange={(e) => { edited.current = true; setTask(e.target.value); setReviewed(false); }}
                onKeyDown={(e) => { if (e.key === "Enter" && (e.ctrlKey || e.metaKey) && !e.nativeEvent.isComposing) { e.preventDefault(); submit(); } }}
                placeholder="Describe the change, scope, constraints and acceptance criteria you authorize." />
              <FieldDescription>The issue is context. This prompt is your explicit instruction.</FieldDescription>
            </Field>
            {issue && <details className="text-xs"><summary className="cursor-pointer rounded-sm font-medium focus-visible:outline-2 focus-visible:outline-ring">Issue source from GitHub (untrusted)</summary>
              <p className="mt-2 text-muted-foreground">External content, not authority to run commands. Version: <span className="break-all">{issue.updated_at}</span></p>
              <p className="mt-2 break-words font-medium">{issue.title}</p>
              <pre tabIndex={0} aria-label="Untrusted issue body" className="mt-1 max-h-48 overflow-auto rounded-md border bg-muted/30 p-3 font-sans whitespace-pre-wrap break-words">{issue.body || "No issue description."}</pre>
            </details>}
            <label className="flex items-start gap-2 text-sm"><Checkbox checked={reviewed && reviewedSource === issue?.updated_at} disabled={action.busy || !issue} onCheckedChange={(checked) => { setReviewed(checked); setReviewedSource(issue?.updated_at ?? null); }} /><span>I reviewed the source and prompt, and authorize {pending ? "retrying this exact request" : "starting work in a new owned worktree"}. Nothing is pushed or published.</span></label>
            <p className="text-xs text-muted-foreground">Agent processes survive closing the GUI, but not daemon exit or reboot.</p>
          </>}
          <ErrorNote error={action.error} />
        </SheetPanel>
        <SheetFooter>
          {forget ? <>
            <Button type="button" variant="outline" disabled={action.busy} onClick={() => setForget(false)}>Keep retry</Button>
            <Button type="button" variant="destructive" disabled={action.busy} onClick={() => void action.run(async () => {
              if (readAttempt(repo, number)?.key === pending?.key) localStorage.removeItem(attemptKey(repo, number));
              onChanged(); onClose();
            })}>Forget retry, not the run</Button>
          </> : <>
            {pending && <Button type="button" variant="ghost" disabled={action.busy} onClick={() => setForget(true)}>Forget retry…</Button>}
            <Button type="button" variant="outline" disabled={action.busy} onClick={onClose}>Cancel</Button>
            <Button type="submit" disabled={action.busy || !ready}>{action.busy ? "Working…" : pending ? "Retry exact request" : <>Start agent <Kbd>⌘↵</Kbd></>}</Button>
          </>}
        </SheetFooter>
      </form>
    </SheetPopup>
  </Sheet>;
}

function IssueRunReview({ run, onChanged, onRetry }: { run: IssueAgentRun; onChanged: () => void; onRetry: () => void }) {
  const fetched = useAsync(() => Agents.get(run.sessionId), [run.sessionId], !!run.sessionId);
  const held = useAgents((s) => s.sessions[run.sessionId]);
  const upsert = useAgents((s) => s.upsert);
  useEffect(() => { if (fetched.data) upsert(fetched.data); }, [fetched.data, upsert]);
  const session = fetched.data ? held ?? fetched.data : null;
  const action = useAction();
  const [diff, setDiff] = useState(false);
  const [publication, setPublication] = useState(false);

  return <div className="flex min-w-0 flex-col gap-3">
    <dl className="grid min-w-0 grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-xs">
      <dt className="text-muted-foreground">Project</dt><dd className="break-all">{run.projectId}</dd>
      <dt className="text-muted-foreground">Worktree</dt><dd className="break-all font-mono">{run.worktree || "Not created"}</dd>
      <dt className="text-muted-foreground">Branch</dt><dd className="break-all font-mono">{run.branch || "Not created"}</dd>
      <dt className="text-muted-foreground">Source version</dt><dd className="break-all">{run.issueUpdatedAt}</dd>
    </dl>
    <details className="text-xs"><summary className="cursor-pointer rounded-sm font-medium focus-visible:outline-2 focus-visible:outline-ring">Reviewed task and original untrusted source</summary>
      <p className="mt-2 whitespace-pre-wrap break-words">{run.task}</p>
      <p className="mt-3 font-medium">Original issue title and body — untrusted GitHub content</p>
      <p className="mt-1 break-words">{run.issueTitle}</p>
      <pre tabIndex={0} aria-label="Original untrusted issue body" className="mt-1 max-h-48 overflow-auto whitespace-pre-wrap break-words font-sans">{run.issueBody || "No issue description."}</pre>
    </details>
    <ErrorNote error={run.error || null} />
    {!run.sessionId && <div className="flex flex-wrap items-center gap-2"><p className="text-xs text-muted-foreground">No agent session has been attached to this request.</p><Button size="xs" variant="outline" onClick={onRetry}>Review retry…</Button></div>}
    {run.sessionId && <>
      {fetched.loading && <Loading what="agent status" />}
      {fetched.error && <ErrorBox error={fetched.error} onRetry={fetched.reload} />}
      {session && <>
        <div className="flex flex-wrap items-center gap-2"><SessionBadge session={session} /><span className="text-xs text-muted-foreground">{session.live ? "Agent process is live" : "Agent process is stopped"}</span></div>
        {session.activity && <p className="break-words text-xs text-muted-foreground">{session.activity}</p>}
        <ErrorNote error={session.error || null} />
        <div className="flex flex-wrap gap-2">
          <Button size="xs" variant="outline" onClick={() => { useAgents.getState().setOpenId(session.id); useUI.getState().setView("agents"); useGitHub.getState().open({ kind: "issue", repo: run.repo, number: run.number }); }}>Open progress & transcript</Button>
          <Button size="xs" variant="outline" aria-expanded={diff} onClick={() => setDiff(!diff)}>{diff ? "Hide worktree diff" : "Review worktree diff"}</Button>
          {session.live && <Button size="xs" variant="outline" disabled={action.busy} onClick={() => void action.run(async () => { upsert(await Agents.stop(session.id)); fetched.reload(); onChanged(); })}>{action.busy ? "Stopping…" : "Stop agent"}</Button>}
          <Button size="xs" variant="ghost" disabled={fetched.loading || action.busy} onClick={fetched.reload}><RefreshCw data-icon="inline-start" />Refresh status</Button>
        </div>
        {diff && <div className="flex h-96 min-h-0 min-w-0 flex-col"><SessionDiff session={session} /></div>}
      </>}
      <p className="text-xs text-muted-foreground">Before preparing a draft PR: stop the agent, review all changes, and commit them in the owned worktree using your existing Git tools. The worktree must be clean, with committed changes against your chosen local base branch. Preparation never commits or pushes.</p>
      {!run.prUrl && <Button size="sm" variant="outline" className="self-start" disabled={!session || session.live || action.busy || fetched.loading || !!fetched.error} onClick={() => setPublication(true)}>Prepare draft PR…</Button>}
    </>}
    {run.prUrl && <a href={run.prUrl} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 self-start text-sm underline underline-offset-4">Open published draft PR<ExternalLink aria-hidden className="size-3.5" /></a>}
    <ErrorNote error={action.error} />
    {publication && <IssuePublication run={run} onClose={() => setPublication(false)} onChanged={onChanged} />}
  </div>;
}

function IssuePublication({ run, onClose, onChanged }: { run: IssueAgentRun; onClose: () => void; onChanged: () => void }) {
  const id = useId();
  const [title, setTitle] = useState(run.issueTitle);
  const [body, setBody] = useState(`Closes #${run.number}`);
  const [base, setBase] = useState(run.baseRef.replace(/^refs\/heads\//, ""));
  const [prepared, setPrepared] = useState<IssuePRPreparation | null>(null);
  const [confirmed, setConfirmed] = useState(false);
  const action = useAction();

  return <Dialog open onOpenChange={(open) => { if (!open && !action.busy) onClose(); }}>
    <DialogPopup className="max-w-3xl" showCloseButton={!action.busy}>
      <form className="contents" onSubmit={(e) => {
        e.preventDefault();
        if (prepared) {
          if (!confirmed) return;
          void action.run(async () => {
            try {
              await GitHub.publishIssuePR(run.id, prepared.token, true);
              onClose();
            } finally {
              onChanged();
            }
          });
        } else if (title.trim() && base.trim()) {
          void action.run(async () => { setConfirmed(false); setPrepared(await GitHub.prepareIssuePR(run.id, title.trim(), body, base.trim())); onChanged(); });
        }
      }}>
        <DialogHeader>
          <DialogTitle>{prepared ? "Review and publish a draft PR" : "Prepare a draft PR"}</DialogTitle>
          <DialogDescription>{run.repo} · {prepared ? "Confirm the exact commit, diff and PR text below. This is the only step that pushes and publishes." : "Stop the agent and commit reviewed changes first. The daemon requires a clean owned worktree and a nonempty diff against the selected local base branch."}</DialogDescription>
        </DialogHeader>
        <DialogPanel className="flex min-w-0 flex-col gap-4">
          {prepared ? <>
            <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-xs">
              <dt>Repository</dt><dd className="break-all">{prepared.run.repo}</dd>
              <dt>Branch</dt><dd className="break-all font-mono">{prepared.run.branch}</dd>
              <dt>Commit to push</dt><dd className="break-all font-mono">{prepared.head}</dd>
              <dt>Base</dt><dd className="break-all font-mono">{prepared.base}</dd>
              <dt>Base commit</dt><dd className="break-all font-mono">{prepared.baseHead}</dd>
              <dt>Title</dt><dd className="break-words">{prepared.title}</dd>
            </dl>
            <section aria-label="Prepared PR body"><h4 className="mb-1 text-sm font-medium">PR description</h4><pre className="whitespace-pre-wrap break-words font-sans text-sm">{prepared.body || "No description."}</pre></section>
            <section aria-label="Exact prepared diff" className="min-w-0"><h4 className="mb-1 text-sm font-medium">Committed diff to publish</h4><pre tabIndex={0} aria-label="Prepared committed diff" className="max-h-96 overflow-auto rounded-md border bg-muted/30 p-3 text-xs">{prepared.diff}</pre></section>
            <p className="text-xs text-muted-foreground">Prepared from a stopped session and a clean, committed worktree. The daemon checks the snapshot again before publication. Any changed branch, commit, base or diff requires preparing and reviewing again.</p>
            <label className="flex items-start gap-2 text-sm"><Checkbox checked={confirmed} disabled={action.busy} onCheckedChange={setConfirmed} /><span>I reviewed this exact diff, commit, destination and PR text. Push this commit and create a draft PR on GitHub as me. Do not merge.</span></label>
            <p className="text-xs text-muted-foreground">If publication fails after a push or the response is lost, retry this same preparation. The daemon checks for an existing PR before creating another.</p>
          </> : <>
            <p className="break-all text-xs text-muted-foreground">Owned worktree: <code>{run.worktree}</code></p>
            <Field><FieldLabel htmlFor={`${id}-title`}>PR title</FieldLabel><Input id={`${id}-title`} autoFocus required disabled={action.busy} value={title} onChange={(e) => setTitle(e.target.value)} /></Field>
            <Field><FieldLabel htmlFor={`${id}-body`}>PR description</FieldLabel><Textarea id={`${id}-body`} disabled={action.busy} rows={5} value={body} onChange={(e) => setBody(e.target.value)} /><FieldDescription>Review the title copied from the untrusted issue. The description is published as Markdown.</FieldDescription></Field>
            <Field><FieldLabel htmlFor={`${id}-base`}>Local base branch</FieldLabel><Input id={`${id}-base`} required disabled={action.busy} value={base} onChange={(e) => setBase(e.target.value)} placeholder="main" /><FieldDescription>Choose an existing local branch that is an ancestor of the owned worktree's committed HEAD.</FieldDescription></Field>
            <p className="text-xs text-muted-foreground">Preparing reads the real committed diff. It does not stop the agent, commit changes, push, or create a PR.</p>
            <p className="text-xs text-muted-foreground">Publishing also needs working Git credentials for HTTPS GitHub pushes, such as your configured credential manager. GitHub API sign-in (including a PAT) does not supply Git push credentials. Push errors are shown without publishing a PR.</p>
          </>}
          <ErrorNote error={action.error} />
        </DialogPanel>
        <DialogFooter>
          <Button type="button" variant="outline" disabled={action.busy} onClick={onClose}>Close</Button>
          {prepared && <Button type="button" variant="outline" disabled={action.busy} onClick={() => { setPrepared(null); setConfirmed(false); action.setError(null); }}>Edit and prepare again</Button>}
          <Button type="submit" disabled={action.busy || (prepared ? !confirmed || !prepared.diff || !prepared.token : !title.trim() || !base.trim())}>{action.busy ? (prepared ? "Publishing…" : "Preparing…") : prepared ? "Push & publish draft PR" : "Prepare committed diff"}</Button>
        </DialogFooter>
      </form>
    </DialogPopup>
  </Dialog>;
}
