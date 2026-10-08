import { useEffect, useState } from "react";
import { Download, FolderOpen, Lock, Search, Star } from "lucide-react";
import { GitHub, events, type GitHubCloneEvent, type Repo } from "@/mygo";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Progress } from "@/components/ui/progress";
import { AlertDialog, AlertDialogClose, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogPopup, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Header, ErrorBox, EmptyLine, Loading, RefreshButton, agoIso, useAsync } from "./shared";
import { errMsg, useGitHub } from "./store";

function useDebounced<T>(v: T, ms: number): T {
  const [d, setD] = useState(v);
  useEffect(() => { const t = setTimeout(() => setD(v), ms); return () => clearTimeout(t); }, [v, ms]);
  return d;
}

/** Repositories: yours by default, GitHub-wide search when a query is typed (server side). */
export function ReposTab() {
  const [q, setQ] = useState("");
  const query = useDebounced(q.trim(), 300);
  const { repo: current, setRepo, setTab } = useGitHub();
  const a = useAsync(() => GitHub.repos(query, 60), [query]);
  const [cloning, setCloning] = useState<Repo | null>(null);
  const [events_, setEvents] = useState<Record<string, GitHubCloneEvent>>({});

  useEffect(() => events.githubClone.on((e) => setEvents((s) => ({ ...s, [e.repo]: e }))), []);

  const pick = (r: Repo, tab: "issues" | "pulls" | "actions") => { setRepo(r.full_name); setTab(tab); };
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <Header title={query ? "Search results" : "Your repositories"} count={a.data && <Badge size="sm" variant="outline">{a.data.length}</Badge>}>
        <div className="relative w-64">
          <Search className="pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input size="sm" type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search all of GitHub…" aria-label="Search repositories" className="ps-6" />
        </div>
        <RefreshButton a={a} />
      </Header>
      <div className="min-h-0 flex-1 overflow-auto">
        {a.error && <ErrorBox error={a.error} onRetry={a.reload} />}
        {a.loading && !a.data && <Loading what="repositories" />}
        {a.data && a.data.length === 0 && <EmptyLine>No repositories{query ? ` match “${query}”` : ""}.</EmptyLine>}
        <ul className="divide-y">
          {a.data?.map((r) => {
            const ev = events_[r.full_name];
            return (
              <li key={r.id} className="flex items-start gap-3 px-3 py-2 hover:bg-accent/40">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-1.5">
                    <button className="truncate font-medium text-sm hover:underline" onClick={() => pick(r, "issues")}>{r.full_name}</button>
                    {r.private && <Lock className="size-3 text-muted-foreground" aria-label="private" />}
                    {r.fork && <Badge size="sm" variant="outline">fork</Badge>}
                    {r.archived && <Badge size="sm" variant="warning">archived</Badge>}
                    {r.full_name === current && <Badge size="sm" variant="info">selected</Badge>}
                  </div>
                  {r.description && <p className="truncate text-muted-foreground text-xs">{r.description}</p>}
                  <p className="mt-0.5 flex gap-3 text-[11px] text-muted-foreground">
                    {r.language && <span>{r.language}</span>}
                    <span className="inline-flex items-center gap-0.5"><Star className="size-3" />{r.stargazers_count}</span>
                    <span>{r.open_issues_count} open issues</span><span>pushed {agoIso(r.pushed_at)}</span>
                  </p>
                  {ev && ev.state === "running" && <div className="mt-1 flex items-center gap-2"><Progress value={ev.percent >= 0 ? ev.percent : null} className="w-40" /><span className="truncate text-[11px] text-muted-foreground">{ev.line}</span></div>}
                  {ev?.state === "done" && <p className="mt-1 text-[11px] text-success-foreground">Cloned to <span className="font-mono">{ev.target}</span></p>}
                  {ev?.state === "error" && <p role="alert" className="mt-1 text-[11px] text-destructive-foreground">{ev.error}</p>}
                </div>
                <div className="flex shrink-0 gap-1">
                  <Button size="xs" variant="outline" onClick={() => pick(r, "pulls")}>Pull requests</Button>
                  <Button size="xs" variant="outline" onClick={() => pick(r, "actions")}>Actions</Button>
                  <Button size="xs" variant="outline" onClick={() => setCloning(r)} disabled={ev?.state === "running"}><Download />Clone…</Button>
                </div>
              </li>
            );
          })}
        </ul>
      </div>
      <CloneDialog repo={cloning} onClose={() => setCloning(null)} />
    </div>
  );
}

/** Clone needs a destination and an explicit confirmation; the daemon audits it. */
function CloneDialog({ repo, onClose }: { repo: Repo | null; onClose: () => void }) {
  const [dest, setDest] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const go = () => {
    if (!repo) return;
    setBusy(true); setErr(null);
    GitHub.clone(repo.full_name, dest.trim(), true).then(onClose, (e) => setErr(errMsg(e))).finally(() => setBusy(false));
  };
  return (
    <AlertDialog open={repo !== null} onOpenChange={(o) => { if (!o && !busy) onClose(); }}>
      <AlertDialogPopup>
        <AlertDialogHeader>
          <AlertDialogTitle>Clone {repo?.full_name}?</AlertDialogTitle>
          <AlertDialogDescription render={<div />}>Runs <code className="font-mono">git clone</code> into a new folder named <code className="font-mono">{repo?.name}</code> inside the directory below. Private repositories use your own git credentials.</AlertDialogDescription>
        </AlertDialogHeader>
        <div className="flex flex-col gap-2 px-6 pb-4">
          <label className="text-sm" htmlFor="gh-clone-dest">Parent directory (absolute path)</label>
          <div className="flex items-center gap-2"><FolderOpen className="size-4 text-muted-foreground" /><Input id="gh-clone-dest" autoFocus value={dest} onChange={(e) => setDest(e.target.value)} placeholder="C:\code\projects" className="font-mono" /></div>
          {err && <p role="alert" className="text-destructive-foreground text-sm">{err}</p>}
        </div>
        <AlertDialogFooter>
          <AlertDialogClose render={<Button variant="outline" disabled={busy} />}>Cancel</AlertDialogClose>
          <Button disabled={busy || !/^([a-zA-Z]:[\\/]|\\\\|\/)/.test(dest.trim())} onClick={go}>{busy ? "Starting…" : "Clone"}</Button>
        </AlertDialogFooter>
      </AlertDialogPopup>
    </AlertDialog>
  );
}
