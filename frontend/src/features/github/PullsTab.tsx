import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { useEffect, useMemo, useState } from "react";
import { ExternalLink, GitBranch, MessageSquare } from "lucide-react";
import { GitHub, type PullRequest } from "@/mygo";
import { DiffView } from "@/components/diff/DiffView";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { Markdown } from "./Markdown";
import { NoRepo } from "./IssuesTab";
import { pullFilesToDiff } from "./patch";
import { CiBadge, EmptyLine, ErrorBox, Header, LabelChip, Loading, PullStateIcon, RefreshButton, ReviewBadge, agoIso, useAsync } from "./shared";
import { useGitHub } from "./store";

const STATES = ["open", "closed", "all"] as const;

function RowStatus({ repo, number }: { repo: string; number: number }) {
  const s = useAsync(() => GitHub.pullStatus(repo, number), [repo, number]);
  if (s.error) return <Tooltip><TooltipTrigger render={<span tabIndex={0}  className="text-[11px] text-muted-foreground" />}>status unavailable</TooltipTrigger><TooltipPopup><span className="max-w-sm whitespace-pre-wrap break-words">{s.error}</span></TooltipPopup></Tooltip>;
  if (!s.data) return <span className="text-[11px] text-muted-foreground">…</span>;
  const c = s.data.checks;
  return (
    <span className="flex items-center gap-1">
      <ReviewBadge state={s.data.reviewState} />
      {c.total === 0 ? <Badge variant="outline">no checks</Badge> : c.failed > 0 ? <Badge variant="error">{c.failed}/{c.total} failed</Badge> : c.pending > 0 ? <Badge variant="warning">{c.pending} running</Badge> : <Badge variant="success">checks passed</Badge>}
    </span>
  );
}

export function PullsTab() {
  const { repo, pending, clearPending } = useGitHub();
  const [state, setState] = useState<(typeof STATES)[number]>("open");
  const [sel, setSel] = useState<number | null>(null);
  const list = useAsync(() => (repo ? GitHub.pulls(repo, state, 30) : Promise.resolve([])), [repo, state]);

  useEffect(() => setSel(null), [repo]);
  useEffect(() => {
    if (pending?.kind === "pr" && pending.repo === repo) { setSel(pending.number); clearPending(); }
  }, [pending, repo, clearPending]);

  if (!repo) return <NoRepo what="pull requests" />;
  return (
    <div className="flex min-h-0 flex-1">
      <section aria-label="Pull requests" className="flex w-[26rem] shrink-0 flex-col border-r">
        <Header title="Pull requests" count={list.data && <Badge size="sm" variant="outline">{list.data.length}</Badge>}>
          <div role="group" aria-label="State" className="flex rounded-md border">
            {STATES.map((s) => <button key={s} aria-pressed={state === s} onClick={() => setState(s)} className={cn("px-2 py-0.5 text-xs capitalize first:rounded-l-md last:rounded-r-md", state === s ? "bg-accent" : "hover:bg-accent/50")}>{s}</button>)}
          </div>
          <RefreshButton a={list} />
        </Header>
        <div className="min-h-0 flex-1 overflow-auto">
          {list.error && <ErrorBox error={list.error} onRetry={list.reload} />}
          {list.loading && !list.data && <Loading what="pull requests" />}
          {list.data?.length === 0 && <EmptyLine>No {state === "all" ? "" : state} pull requests.</EmptyLine>}
          <ul>
            {list.data?.map((p) => (
              <li key={p.id}>
                <button onClick={() => setSel(p.number)} aria-current={sel === p.number ? "true" : undefined} className={cn("flex w-full gap-2 border-b px-3 py-2 text-left outline-none hover:bg-accent/50 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset", sel === p.number && "bg-accent")}>
                  <span className="mt-0.5"><PullStateIcon pr={p} /></span>
                  <span className="min-w-0 flex-1">
                    <span className="block text-sm">{p.title}</span>
                    <span className="text-[11px] text-muted-foreground">#{p.number} · {p.user.login} · {agoIso(p.updated_at)}</span>
                    {p.state === "open" && <span className="mt-1 block"><RowStatus repo={repo} number={p.number} /></span>}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </div>
      </section>
      <div className="min-w-0 flex-1 overflow-auto">
        {sel === null ? <EmptyLine>Select a pull request to review it.</EmptyLine> : <PullDetail key={`${repo}#${sel}`} repo={repo} number={sel} />}
      </div>
    </div>
  );
}

function PullDetail({ repo, number }: { repo: string; number: number }) {
  const pr = useAsync(() => GitHub.pull(repo, number), [repo, number]);
  const status = useAsync(() => GitHub.pullStatus(repo, number), [repo, number]);
  const files = useAsync(() => GitHub.pullFiles(repo, number), [repo, number]);
  const diff = useMemo(() => pullFilesToDiff(files.data ?? []), [files.data]);

  if (pr.error && !pr.data) return <ErrorBox error={pr.error} onRetry={pr.reload} />;
  if (!pr.data) return <Loading what="pull request" />;
  const p: PullRequest = pr.data;
  return (
    <article className="flex min-h-full flex-col gap-3 p-4">
      <header>
        <div className="flex items-start gap-2">
          <span className="mt-1"><PullStateIcon pr={p} /></span>
          <h2 className="flex-1 break-words font-semibold text-lg leading-snug">{p.title} <span className="font-normal text-muted-foreground">#{p.number}</span></h2>
          <a href={p.html_url} target="_blank" rel="noopener noreferrer" aria-label="Open on GitHub" className="inline-flex size-6 items-center justify-center rounded-md hover:bg-accent"><ExternalLink className="size-3.5" /></a>
        </div>
        <p className="mt-1 flex flex-wrap items-center gap-2 text-muted-foreground text-xs">
          <span>{p.user.login} wants to merge</span>
          <span className="inline-flex items-center gap-1 font-mono"><GitBranch className="size-3" />{p.head.ref}</span><span>into</span><span className="font-mono">{p.base.ref}</span>
          <span className="font-mono"><span className="text-success-foreground">+{p.additions}</span> <span className="text-destructive-foreground">-{p.deletions}</span></span>
          <span className="inline-flex items-center gap-0.5"><MessageSquare className="size-3" />{p.comments + p.review_comments}</span>
        </p>
        {p.labels.length > 0 && <div className="mt-2 flex flex-wrap gap-1">{p.labels.map((l) => <LabelChip key={l.id} label={l} />)}</div>}
      </header>

      <section aria-label="Review and checks" className="rounded-lg border bg-card p-3">
        {status.error && <p role="alert" className="text-destructive-foreground text-sm">{status.error}</p>}
        {!status.data && !status.error && <Loading what="review state" />}
        {status.data && (
          <>
            <div className="flex flex-wrap items-center gap-2"><ReviewBadge state={status.data.reviewState} /><span className="text-muted-foreground text-xs">{status.data.reviews.length} reviews · {status.data.checks.passed} passed · {status.data.checks.failed} failed · {status.data.checks.pending} pending</span></div>
            {status.data.checkRuns.length > 0 && (
              <ul className="mt-2 divide-y rounded-md border">
                {status.data.checkRuns.map((c) => (
                  <li key={c.id} className="flex items-center gap-2 px-2 py-1 text-xs"><CiBadge status={c.status} conclusion={c.conclusion} /><span className="truncate">{c.name}</span>{c.output.title && <span className="truncate text-muted-foreground">{c.output.title}</span>}</li>
                ))}
              </ul>
            )}
          </>
        )}
      </section>

      <div className="rounded-lg border bg-card p-3"><Markdown source={p.body} /></div>

      <section aria-label="Files changed" className="flex min-h-0 flex-col">
        <h3 className="mb-1 font-medium text-[13px]">Files changed ({p.changed_files})</h3>
        {files.error && <ErrorBox error={files.error} onRetry={files.reload} />}
        {files.loading && !files.data && <Loading what="diff" />}
        {files.data && <DiffView files={diff} truncated={p.changed_files > files.data.length} className="h-[32rem]" />}
        {files.data && p.changed_files > files.data.length && <p className="mt-1 text-muted-foreground text-xs">GitHub returns at most {files.data.length} files; the rest are on github.com.</p>}
      </section>
    </article>
  );
}
