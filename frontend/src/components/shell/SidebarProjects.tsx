import { useEffect, useMemo, useState } from "react";
import { ChevronRight, Folder, FolderPlus, ListTodo, MoreHorizontal, Plus, SquarePen, TriangleAlert } from "lucide-react";
import type { Project, Session } from "@/mygo";
import { cn } from "@/lib/utils";
import { useAgents } from "@/stores/agents";
import { useProjects } from "@/stores/projects";
import { useUI } from "@/stores/ui";
import { useBoxes } from "@/stores/boxes";
import { ProviderLogo } from "@/features/agents/ProviderLogo";
import { ConfirmDialog } from "@/features/projects/dialogs";
import { countNeedsYou, groupTasksByProject, openProjectTasks, openTask } from "@/features/projects/tasks";
import { sessionTitle } from "@/features/agents/format";
import { Badge } from "@/components/ui/badge";
import { Menu, MenuItem, MenuPopup, MenuSeparator, MenuTrigger } from "@/components/ui/menu";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";

const SHOWN = 5;

const rowBase = "group/row relative flex h-side-row w-full items-center gap-1.5 rounded-md pr-1 text-left outline-none hover:bg-sidebar-accent focus-visible:ring-2 focus-visible:ring-sidebar-ring";
const iconButton = "inline-flex size-5 shrink-0 items-center justify-center rounded text-muted-foreground opacity-0 outline-none hover:text-foreground focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-sidebar-ring group-hover/row:opacity-100 data-popup-open:opacity-100";

const dot: Record<string, string> = {
  working: "bg-info animate-pulse motion-reduce:animate-none",
  "needs-you": "bg-warning",
  failed: "bg-destructive",
  done: "bg-success/70",
};

/**
 * Projects after Shipyard's sidebar tree. A project expands to its associated tasks, newest first.
 * Everything shown is real daemon data; with no projects it offers "Add a project".
 */
export function SidebarProjects() {
  const projects = useProjects((s) => s.projects);
  const loaded = useProjects((s) => s.loaded);
  const error = useProjects((s) => s.error);
  const available = useProjects((s) => s.available);
  const expanded = useProjects((s) => s.expanded);
  const toggle = useProjects((s) => s.toggle);
  const setNewProjectOpen = useProjects((s) => s.setNewProjectOpen);
  const startSync = useProjects((s) => s.startSync);
  const sessions = useAgents((s) => s.sessions);
  const tasks = useMemo(() => groupTasksByProject(Object.values(sessions), projects), [sessions, projects]);

  useEffect(() => startSync(), [startSync]);

  const add = () => setNewProjectOpen(true);

  return (
    <section aria-label="Projects" className="mt-1 flex min-h-0 flex-1 flex-col overflow-auto">
      <div className="flex h-6 shrink-0 items-center gap-1 px-1 text-muted-foreground text-xs">
        <span className="flex-1">Projects</span>
        {available && (
          <button type="button" aria-label="New project" onClick={add} className="inline-flex size-5 items-center justify-center rounded text-muted-foreground outline-none hover:bg-sidebar-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring"><Plus className="size-3.5" /></button>
        )}
      </div>
      {!available ? (
        <p className="px-2 py-1 text-muted-foreground text-xs">Projects need the desktop app (no daemon in browser preview).</p>
      ) : error ? (
        <button type="button" onClick={() => void useProjects.getState().load()} className="mx-1 mt-1 flex items-start gap-1.5 rounded-md px-1.5 py-1 text-left text-destructive-foreground text-xs hover:bg-sidebar-accent"><TriangleAlert className="mt-px size-3 shrink-0" /><span>Could not load projects: {error}. Click to retry.</span></button>
      ) : !loaded ? (
        <p role="status" className="px-2 py-1 text-muted-foreground text-xs">Loading projects…</p>
      ) : projects.length === 0 ? (
        <button type="button" onClick={add} className={cn(rowBase, "mt-1 pl-2 text-muted-foreground text-sm hover:text-foreground")}>
          <FolderPlus className="size-3.5" />Add a project
        </button>
      ) : (
        <ul className="flex flex-col gap-px">
          {projects.map((p) => <ProjectNode key={p.id} project={p} tasks={tasks.get(p.id) ?? []} open={expanded[p.id] !== false} onToggle={() => toggle(p.id)} />)}
        </ul>
      )}
    </section>
  );
}

function ProjectNode({ project, tasks, open, onToggle }: { project: Project; tasks: Session[]; open: boolean; onToggle: () => void }) {
  const remove = useProjects((s) => s.remove);
  const openNewTask = useProjects((s) => s.openNewTask);
  const [all, setAll] = useState(false);
  const [removing, setRemoving] = useState(false);
  const shown = all ? tasks : tasks.slice(0, SHOWN);
  const hidden = tasks.length - shown.length;
  const needsYou = countNeedsYou(tasks);
  const canRun = !project.missing;
  return (
    <li>
      <div className={cn(rowBase, "pl-1 font-medium text-[13px]", project.missing && "text-muted-foreground")}>
        <button type="button" aria-label={`${open ? "Collapse" : "Expand"} ${project.name}`} aria-expanded={open} onClick={onToggle} className="inline-flex size-4 shrink-0 items-center justify-center rounded text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring">
          <ChevronRight className={cn("size-3 transition-transform", open && "rotate-90")} />
        </button>
        <Tooltip><TooltipTrigger render={<button type="button" onClick={onToggle} className="flex min-w-0 flex-1 items-center gap-1.5 self-stretch text-left outline-none" />}>
          <Folder aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
          <span className="min-w-0 flex-1 truncate">{project.name}</span>
          <BoxLabel machine={project.machineId} />
        </TooltipTrigger><TooltipPopup><span className="max-w-sm whitespace-pre-wrap break-words">{`${project.name}\n${project.path || "Managed workspace · no folder needed"}`}</span></TooltipPopup></Tooltip>
        {needsYou > 0 && <Badge size="sm" variant="warning" aria-label={`${needsYou} ${needsYou === 1 ? "task needs" : "tasks need"} you`}>{needsYou}</Badge>}
        <Tooltip><TooltipTrigger render={<button type="button" aria-label={`New task in ${project.name}`} disabled={!canRun} onClick={() => openNewTask(project.id)} className={cn(iconButton, "disabled:pointer-events-none disabled:opacity-0")} />}><SquarePen className="size-3.5" /></TooltipTrigger><TooltipPopup>New task</TooltipPopup></Tooltip>
        <Menu>
          <MenuTrigger render={<button type="button" aria-label={`Actions for ${project.name}`} className={iconButton} />}><MoreHorizontal className="size-3.5" /></MenuTrigger>
          <MenuPopup align="start" side="right" className="min-w-44">
            <MenuItem disabled={!canRun} onClick={() => openNewTask(project.id)}><SquarePen className="size-3.5" />New task…</MenuItem>
            <MenuItem onClick={() => openProjectTasks(project.id)}><ListTodo className="size-3.5" />Open tasks</MenuItem>
            <MenuSeparator />
            <MenuItem variant="destructive" onClick={() => setRemoving(true)}>Remove from Orole…</MenuItem>
          </MenuPopup>
        </Menu>
      </div>
      {open && (
        <ul className="ml-[17px] flex flex-col gap-px border-l py-0.5 pl-1.5">
          {project.missing && <li className="px-2 py-0.5 text-destructive-foreground text-[11px]">Folder not found</li>}
          {tasks.length === 0 ? <li>{canRun ? <button type="button" onClick={() => openNewTask(project.id)} className="flex h-6 w-full items-center px-2 text-left text-muted-foreground text-xs outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring">Start a task</button> : <span className="px-2 py-0.5 text-muted-foreground text-xs">No tasks yet</span>}</li>
            : shown.map((s) => <TaskRow key={s.id} session={s} />)}
          {hidden > 0 && <li><button type="button" onClick={() => setAll(true)} className="flex h-6 w-full items-center px-2 text-left text-muted-foreground text-xs outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring">{hidden} more {hidden === 1 ? "task" : "tasks"}</button></li>}
          {all && tasks.length > SHOWN && <li><button type="button" onClick={() => setAll(false)} className="flex h-6 w-full items-center px-2 text-left text-muted-foreground text-xs outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring">Show fewer</button></li>}
        </ul>
      )}
      <ConfirmDialog open={removing} onOpenChange={setRemoving} title={`Remove ${project.name}?`} confirmLabel="Remove project" onConfirm={() => remove(project.id)}>
        <p>This only forgets the project in Orole OS. Files, branches and worktrees stay on disk.</p>
      </ConfirmDialog>
    </li>
  );
}

function TaskRow({ session }: { session: Session }) {
  const opened = useAgents((s) => s.openId === session.id);
  const inTasks = useUI((s) => s.view === "agents");
  const current = opened && inTasks;
  const title = sessionTitle(session);
  return (
    <li>
      <Tooltip><TooltipTrigger render={<button type="button" aria-current={current ? "page" : undefined} onClick={() => openTask(session.id)} className={cn(rowBase, "pl-2 text-[13px] text-sidebar-foreground", current && "bg-sidebar-accent text-sidebar-accent-foreground")} />}>
        <span role="img" aria-label={session.status} className={cn("size-1.5 shrink-0 rounded-full", dot[session.status] ?? "bg-muted-foreground/50")} />
        <span className="min-w-0 flex-1 truncate">{title}</span>
        <BoxLabel machine={session.machine || "local"} />
        {session.status === "needs-you" && <Badge size="sm" variant="warning">Needs you</Badge>}
        {session.status === "failed" && <Badge size="sm" variant="error">Failed</Badge>}
        <span className="ml-1 inline-flex shrink-0" role="img" aria-label={`${session.kind} agent`}><ProviderLogo kind={session.kind} /></span>
      </TooltipTrigger><TooltipPopup><span className="max-w-sm whitespace-pre-wrap break-words">{`${title}\n${session.status}${session.branch ? ` · ${session.branch}` : ""}`}</span></TooltipPopup></Tooltip>
    </li>
  );
}

function BoxLabel({ machine }: { machine: string }) {
  const name = useBoxes((s) => s.machines.find((m) => m.id === machine)?.name);
  const label = name || (machine === "local" ? "This system" : machine);
  return <span title={`System: ${label}`} className="max-w-16 shrink truncate rounded bg-sidebar-accent px-1 font-mono text-[10px] font-normal text-muted-foreground">{label}</span>;
}
