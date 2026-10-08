import { useCallback, useEffect, useState } from "react";
import { FolderGit2, FolderPlus, GitBranch, RefreshCw, Trash2 } from "lucide-react";
import { Git, type Branch, type Commit, type Project, type Status } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { toDiffFiles, type DiffFile } from "@/lib/diff";
import { ago } from "@/lib/projects";
import { useProjects } from "@/stores/projects";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Spinner } from "@/components/ui/spinner";
import { DiffView } from "@/components/diff/DiffView";
import { NeedsDesktop } from "@/features/shared/States";
import { cn } from "@/lib/utils";
import { ConfirmDialog, CreateWorktreeDialog, errText } from "./dialogs";
import { WorktreeRootField } from "./WorktreeRootField";

export function ProjectsView() {
  const { projects, loaded, error, selectedId, select, remove, load, setNewProjectOpen } = useProjects();
  const [removing, setRemoving] = useState<Project | null>(null);

  useEffect(() => { void load(); }, [load]);
  const current = projects.find((p) => p.id === selectedId) ?? projects[0];
  const add = () => setNewProjectOpen(true);

  if (!inDesktop) return <NeedsDesktop what="Projects" />;
  if (!loaded) return <div role="status" className="flex flex-1 items-center justify-center gap-2 text-muted-foreground text-sm"><Spinner className="size-4" />Loading projects…</div>;
  if (error) return <div className="m-auto flex max-w-md flex-col items-center gap-2 text-center"><p role="alert" className="text-destructive-foreground text-sm">Could not load projects: {error}</p><Button variant="outline" onClick={() => void load()}>Retry</Button></div>;
  if (projects.length === 0) {
    return (
      <Empty>
        <EmptyHeader><EmptyMedia variant="icon"><FolderGit2 /></EmptyMedia><EmptyTitle>Create a project</EmptyTitle>
          <EmptyDescription>Name a project and optionally attach a folder. Folders that are git repositories also get branches and worktrees; nothing is copied or changed.</EmptyDescription></EmptyHeader>
        <EmptyContent><Button onClick={add}><FolderPlus />New project…</Button></EmptyContent>
      </Empty>
    );
  }
  return (
    <div className="flex h-full min-h-0">
      <aside aria-label="Projects" className="flex w-64 shrink-0 flex-col border-r">
        <div className="flex h-9 shrink-0 items-center justify-between border-b px-3"><span className="font-medium text-sm">Registered</span><Button size="xs" variant="outline" onClick={add}><FolderPlus />New</Button></div>
        <ul className="min-h-0 flex-1 overflow-auto p-1">
          {projects.map((p) => (
            <li key={p.id}>
              <button type="button" aria-current={current?.id === p.id ? "true" : undefined} onClick={() => select(p.id)} className={cn("flex w-full flex-col gap-0.5 rounded-md px-2 py-1.5 text-left outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring", current?.id === p.id && "bg-accent")}>
                <span className="flex items-center gap-1.5 text-sm font-medium"><span className="truncate">{p.name}</span><Badge size="sm" variant="outline">{p.machineId}</Badge>{p.missing && <Badge size="sm" variant="error">missing</Badge>}</span>
                <span className="truncate font-mono text-[11px] text-muted-foreground">{p.path || "No folder"}</span>
                <span className="text-[11px] text-muted-foreground">Opened {ago(p.lastOpened)}</span>
              </button>
            </li>
          ))}
        </ul>
      </aside>
      <div className="min-w-0 flex-1 overflow-auto">{current && <ProjectDetail key={current.id} project={current} onRemove={() => setRemoving(current)} />}</div>
      <ConfirmDialog open={removing !== null} onOpenChange={(o) => !o && setRemoving(null)} title={`Remove ${removing?.name ?? "project"}?`} confirmLabel="Remove project" onConfirm={async () => { if (removing) await remove(removing.id); }}>
        <p>This only forgets the project in Orole OS. Files, branches and worktrees stay on disk.</p>
      </ConfirmDialog>
    </div>
  );
}

interface Detail { status: Status; branches: Branch[]; log: Commit[] }

function ProjectDetail({ project, onRemove }: { project: Project; onRemove: () => void }) {
  const [d, setD] = useState<Detail | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [diff, setDiff] = useState<{ files: DiffFile[]; truncated: boolean } | null>(null);
  const [creating, setCreating] = useState(false);

  const refresh = useCallback(() => {
    if (project.missing || !project.isGit) return;
    setBusy(true);
    Promise.all([Git.status(project.id, ""), Git.branches(project.id, ""), Git.log(project.id, "", "", 20)])
      .then(([status, branches, log]) => { setD({ status, branches, log }); setErr(null); })
      .catch((e) => setErr(errText(e))).finally(() => setBusy(false));
  }, [project.id, project.missing, project.isGit]);

  useEffect(() => {
    refresh();
    window.addEventListener("focus", refresh);
    return () => window.removeEventListener("focus", refresh);
  }, [refresh]);

  const showDiff = () => Git.diff({ projectId: project.id, worktreeId: "", staged: false, from: "HEAD", to: "", path: "", context: 3 })
    .then((r) => { setDiff({ files: toDiffFiles(r), truncated: r.truncated }); setErr(null); }, (e) => setErr(errText(e)));

  const st = d?.status;
  return (
    <div className="flex flex-col gap-4 p-4">
      <header className="flex items-start gap-2">
        <div className="min-w-0 flex-1">
          <h2 className="truncate font-medium text-base">{project.name}</h2>
          <p className="truncate font-mono text-muted-foreground text-xs">{project.path || "No folder"}</p>
          {project.remoteUrl && <p className="truncate font-mono text-muted-foreground text-xs">{project.remoteUrl}</p>}
        </div>
        {project.isGit && <Button size="sm" variant="outline" onClick={() => setCreating(true)}><GitBranch />New worktree</Button>}
        {project.isGit && <Button size="sm" variant="outline" onClick={refresh} disabled={busy} aria-label="Refresh">{busy ? <Spinner className="size-3.5" /> : <RefreshCw />}</Button>}
        <Button size="sm" variant="destructive-outline" onClick={onRemove}><Trash2 />Remove</Button>
      </header>
      {project.missing && <p role="alert" className="rounded-md border border-destructive/40 p-3 text-destructive-foreground text-sm">This folder no longer exists. Remove the project or restore the folder.</p>}
      {err && <p role="alert" className="text-destructive-foreground text-sm">{err}</p>}
      {!project.isGit && !project.missing && <p className="text-muted-foreground text-sm">{project.path ? "This folder is not a git repository, so there are no branches or worktrees to show." : "This project has no folder. Git state and worktrees need a folder that is a git repository."}</p>}
      {project.isGit && !d && !err && <p role="status" className="flex items-center gap-2 text-muted-foreground text-sm"><Spinner className="size-4" />Reading git state…</p>}
      {st && d && (
        <>
          <Section title="Status">
            <p className="flex flex-wrap items-center gap-2 text-sm">
              <Badge variant="outline"><GitBranch className="size-3" />{st.branch.head}</Badge>
              {st.branch.upstream && <span className="text-muted-foreground text-xs">tracks {st.branch.upstream}</span>}
              {(st.branch.ahead > 0 || st.branch.behind > 0) && <span className="font-mono text-xs">↑{st.branch.ahead} ↓{st.branch.behind}</span>}
              {st.dirty === 0 ? <Badge variant="success">clean</Badge> : <Badge variant="warning">{st.dirty} changed</Badge>}
              {st.staged > 0 && <span className="text-muted-foreground text-xs">{st.staged} staged</span>}
              {st.unstaged > 0 && <span className="text-muted-foreground text-xs">{st.unstaged} unstaged</span>}
              {st.untracked > 0 && <span className="text-muted-foreground text-xs">{st.untracked} untracked</span>}
              {st.conflicted > 0 && <Badge variant="error">{st.conflicted} conflicted</Badge>}
            </p>
            {st.files.length > 0 && (
              <>
                <ul className="mt-2 max-h-48 overflow-auto rounded-md border font-mono text-xs">
                  {st.files.map((f) => <li key={f.path} className="flex gap-2 px-2 py-0.5 odd:bg-muted/40"><span className="w-6 shrink-0 text-muted-foreground">{f.index === "." ? " " : f.index}{f.worktree === "." ? " " : f.worktree}</span><span className="truncate">{f.path}</span></li>)}
                </ul>
                <Button size="xs" variant="outline" className="mt-2 self-start" onClick={() => void showDiff()}>View changes vs HEAD</Button>
              </>
            )}
            {diff && <DiffView files={diff.files} truncated={diff.truncated} className="mt-2 h-96" />}
          </Section>
          <Section title={`Branches (${d.branches.length})`}>
            <ul className="rounded-md border text-sm">
              {d.branches.map((b) => (
                <li key={`${b.remote}:${b.name}`} className="flex items-center gap-2 border-b px-2 py-1 last:border-b-0">
                  <GitBranch className={cn("size-3.5 shrink-0", b.current ? "text-success" : "text-muted-foreground")} />
                  <span className={cn("truncate font-mono text-xs", b.current && "font-semibold")}>{b.name}</span>
                  {b.remote && <Badge size="sm" variant="outline">remote</Badge>}
                  {(b.ahead > 0 || b.behind > 0) && <span className="font-mono text-[11px] text-muted-foreground">↑{b.ahead} ↓{b.behind}</span>}
                  {b.gone && <Badge size="sm" variant="warning">upstream gone</Badge>}
                  <span className="ml-auto max-w-[40%] truncate text-muted-foreground text-xs">{b.subject}</span>
                </li>
              ))}
            </ul>
          </Section>
          <Section title="Recent commits">
            {d.log.length === 0 ? <p className="text-muted-foreground text-sm">No commits yet.</p> : (
              <ul className="rounded-md border text-sm">
                {d.log.map((c) => (
                  <li key={c.hash} className="flex items-center gap-2 border-b px-2 py-1 last:border-b-0">
                    <code className="shrink-0 font-mono text-[11px] text-muted-foreground">{c.short}</code>
                    <span className="truncate">{c.subject}</span>
                    {c.refs.map((r) => <Badge key={r} size="sm" variant="outline">{r}</Badge>)}
                    <span className="ml-auto shrink-0 text-muted-foreground text-xs">{c.author} · {ago(c.date)}</span>
                  </li>
                ))}
              </ul>
            )}
          </Section>
        </>
      )}
      {project.isGit && <Section title="Worktree location"><WorktreeRootField project={project} /></Section>}
      {project.isGit && <CreateWorktreeDialog project={project} open={creating} onOpenChange={setCreating} />}
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return <section className="flex flex-col gap-1.5"><h3 className="font-medium text-muted-foreground text-xs uppercase tracking-wide">{title}</h3>{children}</section>;
}
