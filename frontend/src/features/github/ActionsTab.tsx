import { useState } from "react";
import { ExternalLink } from "lucide-react";
import { GitHub, type Job, type WorkflowRun } from "@/mygo";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { NoRepo } from "./IssuesTab";
import { CiBadge, EmptyLine, ErrorBox, Header, Loading, RefreshButton, agoIso, useAsync } from "./shared";
import { useGitHub } from "./store";

const FILTERS = [["", "All"], ["failure", "Failed"], ["in_progress", "Running"], ["success", "Passed"]] as const;

export function ActionsTab() {
  const repo = useGitHub((s) => s.repo);
  const [status, setStatus] = useState("");
  const [sel, setSel] = useState<WorkflowRun | null>(null);
  const list = useAsync(() => (repo ? GitHub.runs({ repo, status, branch: "", limit: 40 }) : Promise.resolve([])), [repo, status]);
  if (!repo) return <NoRepo what="workflow runs" />;
  return (
    <div className="flex min-h-0 flex-1">
      <section aria-label="Workflow runs" className="flex w-[26rem] shrink-0 flex-col border-r">
        <Header title="Workflow runs" count={list.data && <Badge size="sm" variant="outline">{list.data.length}</Badge>}>
          <div role="group" aria-label="Status" className="flex rounded-md border">
            {FILTERS.map(([v, l]) => <button key={v} aria-pressed={status === v} onClick={() => setStatus(v)} className={cn("px-2 py-0.5 text-xs first:rounded-l-md last:rounded-r-md", status === v ? "bg-accent" : "hover:bg-accent/50")}>{l}</button>)}
          </div>
          <RefreshButton a={list} />
        </Header>
        <div className="min-h-0 flex-1 overflow-auto">
          {list.error && <ErrorBox error={list.error} onRetry={list.reload} />}
          {list.loading && !list.data && <Loading what="runs" />}
          {list.data?.length === 0 && <EmptyLine>No workflow runs{status ? " with this status" : ""}.</EmptyLine>}
          <ul>
            {list.data?.map((r) => (
              <li key={r.id}>
                <button onClick={() => setSel(r)} aria-current={sel?.id === r.id ? "true" : undefined} className={cn("flex w-full flex-col gap-0.5 border-b px-3 py-2 text-left outline-none hover:bg-accent/50 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset", sel?.id === r.id && "bg-accent")}>
                  <span className="flex items-center gap-2"><CiBadge status={r.status} conclusion={r.conclusion} /><span className="truncate text-sm">{r.display_title}</span></span>
                  <span className="text-[11px] text-muted-foreground">{r.name} #{r.run_number} · {r.head_branch} · {r.event} · {agoIso(r.updated_at)}</span>
                </button>
              </li>
            ))}
          </ul>
        </div>
      </section>
      <div className="min-w-0 flex-1 overflow-auto">
        {sel ? <RunDetail key={sel.id} repo={repo} run={sel} /> : <EmptyLine>Select a run to see its jobs and logs.</EmptyLine>}
      </div>
    </div>
  );
}

function RunDetail({ repo, run }: { repo: string; run: WorkflowRun }) {
  const jobs = useAsync(() => GitHub.runJobs(repo, run.id), [repo, run.id]);
  const failedFirst = [...(jobs.data ?? [])].sort((a, b) => Number(isFailed(b)) - Number(isFailed(a)));
  const firstFailed = failedFirst.find(isFailed);
  const [job, setJob] = useState<Job | null>(null);
  const current = job ?? firstFailed ?? null;
  return (
    <article className="flex min-h-full flex-col gap-3 p-4">
      <header className="flex items-start gap-2">
        <h2 className="flex-1 break-words font-semibold text-base">{run.display_title}</h2>
        <CiBadge status={run.status} conclusion={run.conclusion} />
        <a href={run.html_url} target="_blank" rel="noopener noreferrer" aria-label="Open on GitHub" className="inline-flex size-6 items-center justify-center rounded-md hover:bg-accent"><ExternalLink className="size-3.5" /></a>
      </header>
      {jobs.error && <ErrorBox error={jobs.error} onRetry={jobs.reload} />}
      {jobs.loading && !jobs.data && <Loading what="jobs" />}
      {jobs.data?.length === 0 && <EmptyLine>This run has no jobs.</EmptyLine>}
      <ul className="divide-y rounded-lg border bg-card">
        {failedFirst.map((j) => (
          <li key={j.id}>
            <button onClick={() => setJob(j)} aria-current={current?.id === j.id ? "true" : undefined} className={cn("flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-accent/50", current?.id === j.id && "bg-accent")}>
              <CiBadge status={j.status} conclusion={j.conclusion} /><span className="flex-1 truncate">{j.name}</span>
              {j.steps.filter((s) => s.conclusion === "failure").map((s) => <span key={s.number} className="truncate text-destructive-foreground text-xs">failed: {s.name}</span>)}
            </button>
          </li>
        ))}
      </ul>
      {current && <JobLog key={current.id} repo={repo} run={run} job={current} />}
    </article>
  );
}

const isFailed = (j: Job) => j.status === "completed" && j.conclusion !== "success" && j.conclusion !== "skipped" && j.conclusion !== "neutral";

/** Job log: untrusted text in a mono <pre>; never interpreted. Tail-truncated by the daemon. */
function JobLog({ repo, run, job }: { repo: string; run: WorkflowRun; job: Job }) {
  const log = useAsync(() => GitHub.runLogs(repo, run.id, job.id), [repo, run.id, job.id]);
  return (
    <section aria-label={`Log of ${job.name}`}>
      <h3 className="mb-1 font-medium text-[13px]">Log: {job.name}</h3>
      {log.error && <ErrorBox error={log.error} onRetry={log.reload} />}
      {log.loading && !log.data && <Loading what="log" />}
      {log.data?.map((f) => (
        <div key={f.name}>
          {f.truncated && <p className="mb-1 text-muted-foreground text-xs">Showing the last 256 KB of this log.</p>}
          <pre tabIndex={0} className="selectable max-h-[28rem] overflow-auto rounded-lg border bg-muted/40 p-2 font-mono text-[11px] leading-relaxed whitespace-pre-wrap break-all">{f.text}</pre>
        </div>
      ))}
    </section>
  );
}
