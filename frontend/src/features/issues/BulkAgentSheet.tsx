import { useId, useMemo, useRef, useState } from "react";
import { Bot } from "lucide-react";
import { Agents, GitHub, type Issue, type IssueAgentStart, type Project } from "@/mygo";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { RadioGroup, RadioPrimitive } from "@/components/ui/radio-group";
import { Select, SelectGroup, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetDescription, SheetFooter, SheetHeader, SheetPanel, SheetPopup, SheetTitle } from "@/components/ui/sheet";
import { Textarea } from "@/components/ui/textarea";
import { toastManager } from "@/components/ui/toast";
import { newKey } from "@/features/agents/format";
import { ProviderLogo } from "@/features/agents/ProviderLogo";
import { attemptKey, readAttempt } from "@/features/github/IssueAgentWorkflow";
import { ErrorBox, Loading, useAsync } from "@/features/github/shared";
import { errMsg } from "@/features/github/store";

const LAST_AGENT_KEY = "orole.task.harness";
const DEFAULT_PROMPT = "Resolve {issue}. Use the issue as reference material, add or update tests where they fit, and leave the changes uncommitted for my review.";

type Progress = { state: "running" | "started" | "failed" | "skipped"; message?: string };
const keyOf = (issue: Pick<Issue, "repo" | "number">) => `${issue.repo}#${issue.number}`;

/**
 * Starts one agent per selected issue. Each launch is the same reviewed, idempotent request as the single-issue sheet
 * (own key, saved before dispatch, source version checked by the daemon); issues run one after another, not at once.
 */
export function BulkAgentSheet({ issues, onClose, onStarted }: { issues: Issue[]; onClose: () => void; onStarted: (keys: string[]) => void }) {
  const id = useId();
  const repos = useMemo(() => [...new Set(issues.map((issue) => issue.repo))], [issues]);
  const projects = useAsync(async () => Object.fromEntries(await Promise.all(repos.map(async (repo) => [repo, await GitHub.issueAgentProjects(repo)] as const))) as Record<string, Project[]>, [repos.join(",")]);
  const agents = useAsync(() => Agents.detect(false), []);
  const [projectChoice, setProjectChoice] = useState<Record<string, string>>({});
  const [kindChoice, setKindChoice] = useState("");
  const [prompt, setPrompt] = useState(DEFAULT_PROMPT);
  const [reviewed, setReviewed] = useState(false);
  const [progress, setProgress] = useState<Record<string, Progress>>({});
  const [busy, setBusy] = useState(false);
  const specs = useRef(new Map<string, IssueAgentStart>());
  const locked = specs.current.size > 0;

  const installed = useMemo(() => agents.data?.filter((a) => a.installed && a.kind !== "custom") ?? [], [agents.data]);
  const kind = kindChoice || (installed.find((a) => a.kind === localStorage.getItem(LAST_AGENT_KEY)) ?? installed[0])?.kind || "";
  const usable = (repo: string) => projects.data?.[repo]?.filter((p) => !p.missing) ?? [];
  const projectFor = (repo: string) => projectChoice[repo] || (usable(repo).length === 1 ? usable(repo)[0]!.id : "");
  const pending = issues.filter((issue) => progress[keyOf(issue)]?.state !== "started");
  const launchable = pending.filter((issue) => projectFor(issue.repo));
  const ready = reviewed && !!kind && !!prompt.trim() && launchable.length > 0 && !projects.loading && !agents.loading && !projects.error && !agents.error && !busy;

  const set = (key: string, value: Progress) => setProgress((current) => ({ ...current, [key]: value }));

  const launch = async () => {
    setBusy(true);
    const started: string[] = issues.filter((issue) => progress[keyOf(issue)]?.state === "started").map(keyOf);
    for (const issue of issues) {
      const key = keyOf(issue);
      if (started.includes(key)) continue;
      const projectId = projectFor(issue.repo);
      if (!projectId) { set(key, { state: "skipped", message: "No matching local project" }); continue; }
      set(key, { state: "running" });
      try {
        let spec = specs.current.get(key);
        if (!spec) {
          if (readAttempt(issue.repo, issue.number)) throw new Error("A saved launch is waiting on this issue. Review it from the issue's own page.");
          spec = { repo: issue.repo, number: issue.number, projectId, kind, task: prompt.replaceAll("{issue}", key).trim(), issueUpdatedAt: issue.updated_at, key: newKey(), confirmed: true };
          specs.current.set(key, spec);
        }
        // Persist before dispatch so a lost response can be retried with the same key and never starts a second session.
        localStorage.setItem(attemptKey(issue.repo, issue.number), JSON.stringify(spec));
        await GitHub.startIssueAgent(spec);
        if (readAttempt(issue.repo, issue.number)?.key === spec.key) localStorage.removeItem(attemptKey(issue.repo, issue.number));
        localStorage.setItem(LAST_AGENT_KEY, spec.kind);
        started.push(key);
        set(key, { state: "started" });
      } catch (error) {
        set(key, { state: "failed", message: errMsg(error) });
      }
    }
    setBusy(false);
    if (started.length > 0) toastManager.add({ type: started.length === issues.length ? "success" : "warning", title: `Started ${started.length} ${started.length === 1 ? "agent" : "agents"}`, timeout: 6000 });
    if (started.length === issues.length) { onStarted(started); onClose(); }
  };

  return <Sheet open onOpenChange={(open) => { if (!open && !busy) onClose(); }}>
    <SheetPopup className="max-w-lg" showCloseButton={!busy}>
      <form className="contents" onSubmit={(e) => { e.preventDefault(); if (ready) void launch(); }}>
        <SheetHeader>
          <SheetTitle className="flex items-center gap-2 text-base"><Bot aria-hidden className="size-4 text-muted-foreground" />Start {issues.length} agents</SheetTitle>
          <SheetDescription>One agent and one new worktree per issue, started one after another.</SheetDescription>
        </SheetHeader>
        <SheetPanel className="flex flex-col gap-4">
          {(projects.loading || agents.loading) && <Loading what="projects and agents" />}
          {projects.error && <ErrorBox error={projects.error} onRetry={projects.reload} />}
          {agents.error && <ErrorBox error={agents.error} onRetry={agents.reload} />}
          <ul aria-label="Selected issues" className="flex flex-col gap-1.5">
            {issues.map((issue) => {
              const key = keyOf(issue);
              const state = progress[key];
              const choices = projects.data?.[issue.repo] ?? [];
              const projectId = projectFor(issue.repo);
              return <li key={key} className="flex min-w-0 flex-col gap-1.5 rounded-lg border bg-card px-3 py-2">
                <div className="flex min-w-0 items-start gap-2">
                  <span className="min-w-0 flex-1"><span className="line-clamp-2 font-medium text-[13px] leading-snug">{issue.title}</span><span className="block truncate text-[11px] text-muted-foreground">{key}</span></span>
                  {state && <Badge size="sm" variant={state.state === "started" ? "success" : state.state === "failed" ? "error" : state.state === "running" ? "secondary" : "outline"}>{state.state === "started" ? "Started" : state.state === "running" ? "Starting…" : state.state === "failed" ? "Failed" : "Skipped"}</Badge>}
                </div>
                {state?.message && <p role="status" className="break-words text-destructive-foreground text-xs">{state.message}</p>}
                {projects.data && choices.length === 0 && <p className="text-muted-foreground text-xs">No registered local clone of {issue.repo}. This issue will be skipped.</p>}
                {choices.length > 1 && <Select value={projectId} disabled={locked || busy} onValueChange={(value) => { setProjectChoice((current) => ({ ...current, [issue.repo]: value ?? "" })); setReviewed(false); }}>
                  <SelectTrigger size="sm" aria-label={`Project for ${key}`} className="w-full"><SelectValue>{choices.find((p) => p.id === projectId)?.name ?? "Choose a local project"}</SelectValue></SelectTrigger>
                  <SelectPopup><SelectGroup>{choices.map((p) => <SelectItem key={p.id} value={p.id} disabled={p.missing}>{p.name} · {p.path}{p.missing ? " (missing)" : ""}</SelectItem>)}</SelectGroup></SelectPopup>
                </Select>}
              </li>;
            })}
          </ul>
          <Field>
            <FieldLabel id={`${id}-agent`}>Agent</FieldLabel>
            <RadioGroup aria-labelledby={`${id}-agent`} value={kind} disabled={locked || busy} onValueChange={(v) => { setKindChoice(String(v)); setReviewed(false); }} className="flex w-full flex-row flex-wrap gap-2">
              {installed.map((a) => <RadioPrimitive.Root key={a.kind} value={a.kind} aria-labelledby={`${id}-agent-${a.kind}`} className={cn("flex min-w-0 items-center gap-2 rounded-lg border px-3 py-1.5 text-sm outline-none transition-colors hover:bg-accent/50 focus-visible:ring-2 focus-visible:ring-ring data-checked:border-primary data-checked:bg-accent data-disabled:cursor-not-allowed data-disabled:opacity-60")}>
                <ProviderLogo kind={a.kind} /><span id={`${id}-agent-${a.kind}`} className="truncate">{a.name}</span>
              </RadioPrimitive.Root>)}
            </RadioGroup>
            {agents.data && installed.length === 0 && <p className="text-muted-foreground text-xs">No supported agent is installed.</p>}
          </Field>
          <Field>
            <FieldLabel htmlFor={`${id}-prompt`}>First prompt</FieldLabel>
            <Textarea id={`${id}-prompt`} value={prompt} rows={5} disabled={locked || busy} className="[&_textarea]:font-mono [&_textarea]:text-sm" onChange={(e) => { setPrompt(e.target.value); setReviewed(false); }} />
            <FieldDescription><code className="font-mono">{"{issue}"}</code> becomes each issue's reference, such as <code className="font-mono">acme/shop#12</code>. This prompt is your explicit instruction; the issues are context.</FieldDescription>
          </Field>
          <details className="text-xs"><summary className="cursor-pointer rounded-sm font-medium focus-visible:outline-2 focus-visible:outline-ring">Issue sources from GitHub (untrusted)</summary>
            <div className="mt-2 flex flex-col gap-3">{issues.map((issue) => <div key={keyOf(issue)}>
              <p className="break-words font-medium">{keyOf(issue)} · {issue.title}</p>
              <pre tabIndex={0} aria-label={`Untrusted body of ${keyOf(issue)}`} className="mt-1 max-h-32 overflow-auto rounded-md border bg-muted/30 p-2 font-sans whitespace-pre-wrap break-words">{issue.body || "No issue description."}</pre>
            </div>)}</div>
          </details>
          <label className="flex items-start gap-2 text-sm"><Checkbox checked={reviewed} disabled={busy || launchable.length === 0} onCheckedChange={setReviewed} /><span>I reviewed these issues and the prompt, and authorize starting {launchable.length} {launchable.length === 1 ? "agent" : "agents"}, each in a new owned worktree. Nothing is pushed or published. An issue edited since it was listed is refused.</span></label>
        </SheetPanel>
        <SheetFooter>
          <Button type="button" variant="outline" disabled={busy} onClick={onClose}>{pending.length === 0 ? "Close" : "Cancel"}</Button>
          <Button type="submit" disabled={!ready}>{busy ? "Starting…" : locked ? `Retry ${launchable.length}` : `Start ${launchable.length} ${launchable.length === 1 ? "agent" : "agents"}`}</Button>
        </SheetFooter>
      </form>
    </SheetPopup>
  </Sheet>;
}
