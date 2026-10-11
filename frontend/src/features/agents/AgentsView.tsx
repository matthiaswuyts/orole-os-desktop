import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { memo, useEffect, useMemo, useState } from "react";
import { Plus, RefreshCw, X } from "lucide-react";
import { Agents, type Session, type Run } from "@/mygo";
import { useAgents } from "@/stores/agents";
import { useProjects } from "@/stores/projects";
import { useDaemon } from "@/stores/daemon";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { NeedsDesktop, NotConnected } from "@/features/shared/States";
import { cn } from "@/lib/utils";
import { COLUMN_SPECS, groupColumns } from "./columns";
import { AgentGlyph, StateGlyph, SessionMeta, SessionBadge, PermissionCard, ReplyBox, ErrorNote } from "./shared";
import { isRemoteProcess, sessionTitle, useAction } from "./format";
import { Orchestrate } from "./Orchestrate";
import { TaskView } from "@/features/tasks/TaskView";
import { belongsToProject } from "@/features/projects/tasks";

export function AgentsView() {
  const { available, sessions, loaded, loading, error, offline, refresh, detect, runs, projectFilter, setProjectFilter, openId } = useAgents();
  const link = useDaemon((s) => s.link);
  const projects = useProjects((s) => s.projects);
  const projectsLoaded = useProjects((s) => s.loaded);
  const openNewTask = useProjects((s) => s.openNewTask);
  const project = projectFilter ? projects.find((p) => p.id === projectFilter) : undefined;
  const cols = useMemo(() => groupColumns(project ? Object.values(sessions).filter((s) => belongsToProject(s, project)) : Object.values(sessions)), [sessions, project]);
  const total = COLUMN_SPECS.reduce((sum, c) => sum + cols[c.id].length, 0);
  useEffect(() => { void detect(false); }, [detect]);
  // A removed project must not leave the dashboard filtered to nothing.
  useEffect(() => { if (projectFilter && projectsLoaded && !project) setProjectFilter(null); }, [projectFilter, projectsLoaded, project, setProjectFilter]);
  if (!available) return <NeedsDesktop what="Tasks" />;
  if (offline || link === "offline") return <div className="flex h-full"><NotConnected app="Tasks" what={error || "The daemon is offline. Reconnect to manage tasks."} action={{ label: "Retry", onClick: () => { void useDaemon.getState().refresh(); void refresh(); } }} /></div>;
  return <div className="flex h-full min-h-0 flex-col">{openId && sessions[openId] ? <TaskView key={openId} session={sessions[openId]} /> : <>
    <header className="flex min-h-12 shrink-0 flex-wrap items-center gap-3 border-b bg-sidebar/40 px-6 py-2"><p className="min-w-0 flex-1 text-xs text-muted-foreground">{total} {total === 1 ? "task" : "tasks"} · {cols["needs-you"].length} needs you · {cols.working.length} working</p>{project && <Badge variant="secondary" className="gap-1 pr-0.5">Project: {project.name}<Button size="icon-xs" variant="ghost" className="size-4" aria-label={`Show tasks of every project, not only ${project.name}`} onClick={() => setProjectFilter(null)}><X /></Button></Badge>}<Button size="sm" variant="ghost" disabled={loading} onClick={() => void refresh()} aria-label="Refresh tasks"><RefreshCw /></Button><Button size="sm" onClick={() => openNewTask(project?.id ?? null)}><Plus />New task</Button></header>
    {runs.length > 0 && <div aria-label="Orchestration runs" className="flex shrink-0 gap-2 overflow-x-auto border-b px-6 py-2">{runs.map((run) => <RunItem key={run.id} run={run} />)}</div>}
    {!loaded ? <p role="status" className="p-6 text-muted-foreground">Loading agents…</p> : <div className="min-h-0 flex-1 overflow-auto"><div className="grid grid-cols-[repeat(auto-fit,minmax(250px,1fr))] items-start gap-x-4 gap-y-6 px-6 pt-4 pb-6">{COLUMN_SPECS.map((c) => <section key={c.id} aria-label={c.title} className="min-w-0"><header className="mb-2 flex min-h-5 items-center gap-2 px-0.5"><StateGlyph status={c.id} /><h3 className="text-xs font-medium">{c.title}</h3><span className={cn("ml-auto font-mono text-[11px] tabular-nums", c.id === "needs-you" && cols[c.id].length ? "text-warning-foreground" : "text-muted-foreground")}>{cols[c.id].length}</span></header><div className="flex flex-col gap-row-gap">{cols[c.id].length ? cols[c.id].map((s) => <AgentCard key={s.id} session={s} />) : <p className="rounded-lg border border-dashed px-3 py-4 text-center text-xs text-muted-foreground">{c.empty}</p>}</div></section>)}</div></div>}
  </>}</div>;
}
/** Memoized: a session update re-renders only the card whose session changed. */
const AgentCard = memo(function AgentCard({ session: s }: { session: Session }) {
  const [reply, setReply] = useState(false);
  const open = useAgents((st) => st.setOpenId);
  const remote = isRemoteProcess(s);
  const canReply = remote ? !s.live && s.status !== "lost" : !(s.live && s.mode === "terminal");
  return <article aria-label={sessionTitle(s)} className={cn("flex min-w-0 flex-col rounded-lg border bg-card", s.status === "needs-you" && "border-warning/45", s.status === "failed" && "border-destructive/40")}>
    <div className="flex flex-col gap-2 px-3 pt-row-pad">
      <div className="flex items-center gap-2"><AgentGlyph kind={s.kind} /><Tooltip><TooltipTrigger render={<h4 tabIndex={0} className="min-w-0 flex-1 truncate text-[13px] font-medium"  />}>{sessionTitle(s)}</TooltipTrigger><TooltipPopup><span className="max-w-sm whitespace-pre-wrap break-words">{sessionTitle(s)}</span></TooltipPopup></Tooltip></div>
      <SessionMeta session={s} />
      {(s.status === "failed" || s.status === "lost" || s.status === "stopped" || s.review === "approved") && <div><SessionBadge session={s} /></div>}
      {s.activity && <Tooltip><TooltipTrigger render={<p tabIndex={0} className="truncate text-xs text-muted-foreground"  />}>{s.activity}</TooltipTrigger><TooltipPopup><span className="max-w-sm whitespace-pre-wrap break-words">{s.activity}</span></TooltipPopup></Tooltip>}
      {s.error && <ErrorNote error={s.error} />}
      {s.tail && <div className="rounded-md bg-muted/50 px-2 py-1.5 font-mono text-[11px] leading-snug text-muted-foreground">{s.tail.trimEnd().split(/\r?\n/).slice(-3).map((line, i) => <Tooltip key={i}><TooltipTrigger render={<div tabIndex={0} className="truncate whitespace-pre" />}>{line || " "}</TooltipTrigger><TooltipPopup><span className="max-w-sm whitespace-pre-wrap break-words">{line}</span></TooltipPopup></Tooltip>)}</div>}
      {!remote && s.pending?.map((req) => <PermissionCard key={req.id} sessionId={s.id} request={req} />)}
      {reply && <ReplyBox session={s} onSent={() => setReply(false)} />}
    </div>
    <footer className="mt-2 flex items-center justify-end gap-0.5 border-t px-1.5 py-1">
      <Button size="xs" variant="ghost" onClick={() => open(s.id)}>Open</Button>
      {canReply && <Button size="xs" variant="ghost" onClick={() => setReply((value) => !value)}>{remote ? "New task…" : s.status === "lost" || s.status === "stopped" ? "Resume…" : "Reply…"}</Button>}
      <Orchestrate session={s} onReply={() => setReply(true)} />
    </footer>
  </article>;
});
function RunItem({ run }: { run: Run }) {
  const action = useAction();
  return <div className="flex shrink-0 items-center gap-2 rounded-md border px-2 py-1 text-xs"><span>{run.kind}</span><Badge variant={run.status === "failed" ? "error" : run.status === "succeeded" ? "success" : "secondary"}>{run.status}</Badge>{run.status === "running" && run.kind === "loop-until" && <Button size="xs" variant="ghost" disabled={action.busy} onClick={() => void action.run(async () => { await Agents.cancelRun(run.id); await useAgents.getState().refresh(); })}>Cancel</Button>}<ErrorNote error={action.error || run.error} /></div>;
}
