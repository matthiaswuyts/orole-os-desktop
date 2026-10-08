import { useEffect, type ReactNode } from "react";
import { Bell, CircleDot, GitPullRequest, PlayCircle } from "lucide-react";
import { GitHub } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { useUI } from "@/stores/ui";
import { Widget, type WidgetStatus } from "@/components/widgets/Widget";
import type { WidgetProps } from "@/components/widgets/registry";
import { CiBadge, agoIso, parseSubjectUrl, useAsync, type Async } from "./shared";
import { useGitHub, type OpenItem } from "./store";

/** Common state handling: preview -> offline, not connected -> setup, then real loading/error. */
function useGitHubWidget<T>(fn: () => Promise<T>): { a: Async<T>; status: WidgetStatus; message?: string; action?: { label: string; onClick: () => void } } {
  const { status: conn, error, load, startSync } = useGitHub();
  const setView = useUI((s) => s.setView);
  useEffect(() => { void load(); return startSync(); }, [load, startSync]);
  const connected = conn?.connected === true;
  const a = useAsync(fn, [connected], connected);
  const open = { label: "Open GitHub", onClick: () => setView("github") };
  if (!inDesktop) return { a, status: "offline", message: "Preview mode: no daemon, so no GitHub data." };
  if (error) return { a, status: "error", message: error, action: { label: "Retry", onClick: () => void load() } };
  if (!conn) return { a, status: "loading" };
  if (!connected) return { a, status: "unconfigured", message: "Connect GitHub to see this.", action: { label: "Connect GitHub", onClick: () => setView("github") } };
  if (a.error) return { a, status: "error", message: a.error, action: { label: "Retry", onClick: a.reload } };
  if (!a.data) return { a, status: "loading" };
  return { a, status: "ready", action: open };
}

function openItem(item: OpenItem) {
  useGitHub.getState().open(item);
  useUI.getState().setView("github");
}

function Empty({ children }: { children: ReactNode }) {
  return <p className="p-4 text-center text-muted-foreground text-xs">{children}</p>;
}

function ItemRow({ title, meta, onClick, lead }: { title: string; meta: string; onClick: () => void; lead?: ReactNode }) {
  return (
    <li>
      <button onClick={onClick} className="flex w-full items-start gap-2 rounded px-1 py-1 text-left hover:bg-accent">
        {lead}
        <span className="min-w-0 flex-1"><span className="block truncate">{title}</span><span className="block truncate text-[11px] text-muted-foreground">{meta}</span></span>
      </button>
    </li>
  );
}

export function AssignedIssuesWidget({ onRemove }: WidgetProps) {
  const w = useGitHubWidget(() => GitHub.search("is:issue is:open assignee:@me archived:false", 10));
  const items = w.a.data?.items ?? [];
  return (
    <Widget title="Assigned issues" icon={<CircleDot />} status={w.status} message={w.message} action={w.action} onRemove={onRemove} count={w.a.data && <span className="text-muted-foreground text-xs">{w.a.data.total}</span>}>
      {items.length === 0 ? <Empty>Nothing assigned to you.</Empty> : (
        <ul>{items.map((i) => <ItemRow key={i.id} title={i.title} meta={`${i.repo} #${i.number} · ${agoIso(i.updated_at)}`} onClick={() => openItem({ kind: "issue", repo: i.repo, number: i.number })} lead={<CircleDot className="mt-0.5 size-3.5 shrink-0 text-success-foreground" />} />)}</ul>
      )}
    </Widget>
  );
}

export function OpenPullsWidget({ onRemove }: WidgetProps) {
  const w = useGitHubWidget(() => GitHub.search("is:pr is:open author:@me archived:false", 10));
  const items = w.a.data?.items ?? [];
  return (
    <Widget title="Open pull requests" icon={<GitPullRequest />} status={w.status} message={w.message} action={w.action} onRemove={onRemove} count={w.a.data && <span className="text-muted-foreground text-xs">{w.a.data.total}</span>}>
      {items.length === 0 ? <Empty>You have no open pull requests.</Empty> : (
        <ul>{items.map((i) => <ItemRow key={i.id} title={i.title} meta={`${i.repo} #${i.number} · ${agoIso(i.updated_at)}`} onClick={() => openItem({ kind: "pr", repo: i.repo, number: i.number })} lead={<GitPullRequest className="mt-0.5 size-3.5 shrink-0 text-success-foreground" />} />)}</ul>
      )}
    </Widget>
  );
}

export function GitHubNotificationsWidget({ onRemove }: WidgetProps) {
  const w = useGitHubWidget(() => GitHub.notifications(false, 10));
  const items = w.a.data ?? [];
  return (
    <Widget title="GitHub notifications" icon={<Bell />} status={w.status} message={w.message} action={w.action} onRemove={onRemove} count={w.a.data && <span className="text-muted-foreground text-xs">{items.length}</span>}>
      {items.length === 0 ? <Empty>You are all caught up.</Empty> : (
        <ul>{items.map((n) => {
          const t = parseSubjectUrl(n.subject.url);
          return <ItemRow key={n.id} title={n.subject.title} meta={`${n.repository.full_name} · ${n.reason.replace("_", " ")} · ${agoIso(n.updated_at)}`} onClick={() => (t ? openItem(t) : useUI.getState().setView("github"))} />;
        })}</ul>
      )}
    </Widget>
  );
}

export function FailedCiWidget({ onRemove }: WidgetProps) {
  const w = useGitHubWidget(() => GitHub.failedRuns(6));
  const items = w.a.data ?? [];
  return (
    <Widget title="Failed CI" icon={<PlayCircle />} status={w.status} message={w.message} action={w.action} onRemove={onRemove} count={w.a.data && <span className="text-muted-foreground text-xs">{items.length}</span>}>
      {items.length === 0 ? <Empty>No failed runs in your recent repositories.</Empty> : (
        <ul>{items.map((r) => (
          <ItemRow key={r.id} title={r.display_title} meta={`${r.repo} · ${r.name} · ${r.head_branch} · ${agoIso(r.updated_at)}`}
            lead={<CiBadge status={r.status} conclusion={r.conclusion} />}
            onClick={() => { useGitHub.setState({ tab: "actions", repo: r.repo }); useUI.getState().setView("github"); }} />
        ))}</ul>
      )}
    </Widget>
  );
}
