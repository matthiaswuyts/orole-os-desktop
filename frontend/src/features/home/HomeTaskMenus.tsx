import { ChevronsUpDown, Folder, GitBranch, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Menu, MenuTrigger, MenuPopup, MenuGroup, MenuGroupLabel, MenuRadioGroup, MenuRadioItem, MenuSeparator, MenuItem } from "@/components/ui/menu";
import { boxStatus } from "@/stores/boxes";
import { cn } from "@/lib/utils";
import type { HomeTaskLaunch } from "./useHomeTaskLaunch";

const triggerClass = "h-9 min-w-0 gap-1.5 px-2 font-normal text-muted-foreground";
const arrows = <ChevronsUpDown aria-hidden className="size-3.5 shrink-0 opacity-60" />;

export function ProjectMenu({ task }: { task: HomeTaskLaunch }) {
  return <Menu><MenuTrigger disabled={task.busy} aria-label="Project" render={<Button type="button" variant="ghost" className={triggerClass} />}><Folder aria-hidden /><span className="max-w-32 truncate">{task.project?.name ?? "Project"}</span>{arrows}</MenuTrigger>
    <MenuPopup align="start" className="min-w-60"><MenuGroup><MenuGroupLabel>Projects</MenuGroupLabel><MenuRadioGroup value={task.project?.id ?? ""} onValueChange={(v) => task.chooseProject(String(v))}>
      {task.choices.map((p) => <MenuRadioItem key={p.id} value={p.id} disabled={p.missing}><span className="flex items-center gap-2"><Folder aria-hidden /><span className="truncate">{p.name}</span>{p.missing && <span className="text-xs text-muted-foreground">Missing folder</span>}</span></MenuRadioItem>)}
    </MenuRadioGroup></MenuGroup><MenuSeparator /><MenuItem onClick={task.createProject}><Plus />Create new project…</MenuItem></MenuPopup>
  </Menu>;
}

export function SystemMenu({ task }: { task: HomeTaskLaunch }) {
  const system = task.machines.find((m) => m.id === task.machine);
  const status = boxStatus(task.observations[task.machine], task.now);
  return <Menu><MenuTrigger disabled={task.busy} aria-label="Execution system" render={<Button type="button" variant="ghost" className={triggerClass} />}>
    <span aria-hidden className={cn("size-1.5 shrink-0 rounded-full", status === "online" ? "bg-success" : status === "offline" || status === "error" ? "bg-destructive" : "bg-muted-foreground")} />
    <span className="max-w-24 truncate">{system?.name ?? (task.machine === "local" ? "This system" : task.machine)}</span>{arrows}</MenuTrigger>
    <MenuPopup align="start" className="min-w-56"><MenuGroup><MenuGroupLabel>Execute on</MenuGroupLabel><MenuRadioGroup value={task.machine} onValueChange={(v) => task.chooseMachine(String(v))}>
      {!task.machines.some((m) => m.id === "local") && <MenuRadioItem value="local">This system</MenuRadioItem>}
      {task.machines.map((m) => <MenuRadioItem key={m.id} value={m.id}><span className="flex items-center justify-between gap-4"><span>{m.name}</span><span className="text-xs text-muted-foreground">{boxStatus(task.observations[m.id], task.now)}</span></span></MenuRadioItem>)}
    </MenuRadioGroup></MenuGroup></MenuPopup></Menu>;
}

export function WorktreeMenu({ task }: { task: HomeTaskLaunch }) {
  const current = task.trees.find((w) => w.id === task.worktree);
  const label = task.worktree === "new" ? "New worktree" : current ? current.branch || "Detached HEAD" : task.project?.isGit ? "Current checkout" : "Project workspace";
  return <Menu><MenuTrigger disabled={task.busy || !task.project} aria-label="Worktree" render={<Button type="button" variant="ghost" className={triggerClass} />}><GitBranch aria-hidden /><span className="max-w-40 truncate">{label}</span>{arrows}</MenuTrigger>
    <MenuPopup align="start" className="min-w-60"><MenuGroup><MenuGroupLabel>Worktree</MenuGroupLabel><MenuRadioGroup value={task.worktree} onValueChange={(v) => task.chooseWorktree(String(v))}>
      {task.canIsolate && <MenuRadioItem value="new">New worktree</MenuRadioItem>}
      <MenuRadioItem value="current">{task.project?.isGit ? "Current checkout" : "Project workspace"}</MenuRadioItem>
      {task.trees.filter((w) => !w.isMain).map((w) => <MenuRadioItem key={w.id} value={w.id} disabled={Boolean(w.error)}>{w.branch || "Detached HEAD"}</MenuRadioItem>)}
    </MenuRadioGroup></MenuGroup></MenuPopup></Menu>;
}
