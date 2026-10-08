import { useEffect, useState } from "react";
import { ChevronRight, FolderGit2, FolderPlus, GitBranch, Home, MoreHorizontal, Plus, TriangleAlert } from "lucide-react";
import type { Project, Worktree } from "@/mygo";
import { cn } from "@/lib/utils";
import { useProjects, worktreeState } from "@/stores/projects";
import { useUI } from "@/stores/ui";
import { CreateWorktreeDialog, ConfirmDialog, errText } from "@/features/projects/dialogs";
import { Menu, MenuItem, MenuPopup, MenuSeparator, MenuTrigger } from "@/components/ui/menu";

const SHOWN = 5;

const dot: Record<ReturnType<typeof worktreeState>, string> = {
  error: "bg-destructive", dirty: "bg-warning", ahead: "bg-info", behind: "bg-info/60", clean: "bg-success/60",
};
const dotLabel: Record<ReturnType<typeof worktreeState>, string> = {
  error: "unreadable", dirty: "uncommitted changes", ahead: "commits to push", behind: "behind upstream", clean: "clean",
};

const rowBase = "group/row relative flex h-side-row w-full items-center gap-1.5 rounded-md pr-1 text-left outline-none hover:bg-sidebar-accent focus-visible:ring-2 focus-visible:ring-sidebar-ring";

/**
 * Projects section after Shipyard's sidebar tree. Everything shown is real daemon data: registered
 * projects and their git worktrees. With none registered it shows an "Add a project" row.
 */
export function SidebarProjects() {
  const { projects, worktrees, loaded, error, available, expanded, toggle, pickAndAdd, startSync, select } = useProjects();
  const setView = useUI((s) => s.setView);
  const [addError, setAddError] = useState<string | null>(null);

  useEffect(() => startSync(), [startSync]);

  const add = () => { setAddError(null); pickAndAdd().then((p) => { if (p) { select(p.id); setView("projects"); } }, (e) => setAddError(errText(e))); };

  return (
    <section aria-label="Projects" className="mt-1 flex min-h-0 flex-1 flex-col overflow-auto">
      <div className="group/head flex h-6 shrink-0 items-center gap-1 px-1 text-muted-foreground text-xs">
        <span className="flex-1">Projects</span>
        {available && (
          <Menu>
            <MenuTrigger render={<button type="button" aria-label="Project actions" className="inline-flex size-5 items-center justify-center rounded text-muted-foreground outline-none hover:bg-sidebar-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring" />}><Plus className="size-3.5" /></MenuTrigger>
            <MenuPopup align="end" className="min-w-44">
              <MenuItem onClick={add}><FolderPlus className="size-3.5" />Add existing folder…</MenuItem>
              <MenuSeparator />
              <MenuItem onClick={() => setView("projects")}><FolderGit2 className="size-3.5" />Manage projects</MenuItem>
            </MenuPopup>
          </Menu>
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
          {projects.map((p) => <ProjectNode key={p.id} project={p} wts={worktrees[p.id] ?? []} open={expanded[p.id] !== false} onToggle={() => toggle(p.id)} />)}
        </ul>
      )}
      {addError && <p role="alert" className="px-2 py-1 text-destructive-foreground text-xs">{addError}</p>}
    </section>
  );
}

function ProjectNode({ project, wts, open, onToggle }: { project: Project; wts: Worktree[]; open: boolean; onToggle: () => void }) {
  const setView = useUI((s) => s.setView);
  const select = useProjects((s) => s.select);
  const remove = useProjects((s) => s.remove);
  const openProject = useProjects((s) => s.open);
  const [all, setAll] = useState(false);
  const [creating, setCreating] = useState(false);
  const [removing, setRemoving] = useState(false);
  const shown = all ? wts : wts.slice(0, SHOWN);
  const hidden = wts.length - shown.length;
  const goProject = () => { select(project.id); void openProject(project.id).catch(() => {}); setView("projects"); };
  return (
    <li>
      <div className={cn(rowBase, "pl-1 font-medium text-[13px]", project.missing && "text-muted-foreground")}>
        <button type="button" aria-label={`${open ? "Collapse" : "Expand"} ${project.name}`} aria-expanded={open} onClick={onToggle} className="inline-flex size-4 shrink-0 items-center justify-center rounded text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring">
          <ChevronRight className={cn("size-3 transition-transform", open && "rotate-90")} />
        </button>
        <button type="button" onClick={goProject} title={project.path} className="flex min-w-0 flex-1 items-center gap-1.5 self-stretch outline-none">
          <span className="truncate">{project.name}</span>
          <span className="shrink-0 rounded bg-sidebar-accent px-1 font-normal text-[10px] text-muted-foreground">{project.machineId}</span>
        </button>
        <Menu>
          <MenuTrigger render={<button type="button" aria-label={`Actions for ${project.name}`} className="inline-flex size-5 shrink-0 items-center justify-center rounded text-muted-foreground opacity-0 outline-none hover:text-foreground focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-sidebar-ring group-hover/row:opacity-100 data-popup-open:opacity-100" />}><MoreHorizontal className="size-3.5" /></MenuTrigger>
          <MenuPopup align="start" side="right" className="min-w-44">
            <MenuItem disabled={project.missing} onClick={() => setCreating(true)}><GitBranch className="size-3.5" />New worktree…</MenuItem>
            <MenuItem onClick={goProject}><FolderGit2 className="size-3.5" />Open project</MenuItem>
            <MenuSeparator />
            <MenuItem variant="destructive" onClick={() => setRemoving(true)}>Remove from Orole…</MenuItem>
          </MenuPopup>
        </Menu>
      </div>
      {project.missing && open && <p className="ml-5 px-2 py-0.5 text-destructive-foreground text-[11px]">Folder not found</p>}
      {open && !project.missing && (
        <ul className="ml-[17px] flex flex-col gap-px border-l py-0.5 pl-1.5">
          {shown.map((w) => <WorktreeRow key={w.id} wt={w} />)}
          {hidden > 0 && <li><button type="button" onClick={() => setAll(true)} className="flex h-6 w-full items-center px-2 text-left text-muted-foreground text-xs outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring">{hidden} more {hidden === 1 ? "worktree" : "worktrees"}</button></li>}
          {all && wts.length > SHOWN && <li><button type="button" onClick={() => setAll(false)} className="flex h-6 w-full items-center px-2 text-left text-muted-foreground text-xs outline-none hover:text-foreground">Show fewer</button></li>}
        </ul>
      )}
      <CreateWorktreeDialog project={project} open={creating} onOpenChange={setCreating} />
      <ConfirmDialog open={removing} onOpenChange={setRemoving} title={`Remove ${project.name}?`} confirmLabel="Remove project" onConfirm={() => remove(project.id)}>
        <p>This only forgets the project in Orole OS. Files, branches and worktrees stay on disk.</p>
      </ConfirmDialog>
    </li>
  );
}

function WorktreeRow({ wt }: { wt: Worktree }) {
  const setView = useUI((s) => s.setView);
  const state = worktreeState(wt);
  const Icon = wt.isMain ? Home : GitBranch;
  const label = wt.isMain ? "main checkout" : wt.name;
  return (
    <li>
      <button type="button" onClick={() => setView("worktrees")} title={`${wt.path}\n${wt.branch || "(detached)"}`} className={cn(rowBase, "pl-2 text-[13px] text-sidebar-foreground")}>
        <Icon className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate">{label}</span>
        {(wt.ahead > 0 || wt.behind > 0) && <span className="shrink-0 font-mono text-[10px] text-muted-foreground tabular-nums">{wt.ahead > 0 && `↑${wt.ahead}`}{wt.behind > 0 && `↓${wt.behind}`}</span>}
        <span role="img" aria-label={dotLabel[state]} title={`${wt.branch || "detached"}: ${dotLabel[state]}${wt.dirty ? ` (${wt.dirty})` : ""}`} className={cn("size-1.5 shrink-0 rounded-full", dot[state])} />
      </button>
    </li>
  );
}

