import { FolderIcon, GitBranchIcon, PlusIcon } from "lucide-react";
import { MenuItem } from "@/components/ui/menu";
import { PromptInputSelect } from "@/components/blocks/prompt-input/prompt-controls";
import { boxStatus } from "@/stores/boxes";
import { cn } from "@/lib/utils";
import type { HomeTaskLaunch } from "./useHomeTaskLaunch";

function StatusDot({ status }: { status: string }) {
  return <span aria-hidden className={cn("m-1.25 size-1.5 shrink-0 rounded-full", status === "online" ? "bg-success" : status === "offline" || status === "error" ? "bg-destructive" : "bg-muted-foreground")} />;
}

export function SystemMenu({ task }: { task: HomeTaskLaunch }) {
  const systems = [...(task.machines.some((m) => m.id === "local") ? [] : [{ id: "local", name: "This system" }]), ...task.machines];
  const options = systems.map((m) => {
    const status = boxStatus(task.observations[m.id], task.now);
    return { value: m.id, label: m.name, icon: <StatusDot status={status} />, description: status };
  });
  return <PromptInputSelect label="Run on" heading="Run on" options={options} value={task.machine} disabled={task.busy} onValueChange={task.chooseMachine}
    placeholder={task.machine === "local" ? "This system" : task.machine} />;
}

export function ProjectMenu({ task }: { task: HomeTaskLaunch }) {
  const options = task.choices.map((p) => ({ value: p.id, label: p.name, icon: <FolderIcon aria-hidden />, disabled: p.missing, description: p.missing ? "Missing folder" : undefined }));
  return <PromptInputSelect label="Project" heading="Projects" options={options} value={task.project?.id} disabled={task.busy} onValueChange={task.chooseProject}
    placeholder="Choose project" icon={<FolderIcon aria-hidden />}>
    <MenuItem onClick={task.createProject}><PlusIcon />Create new project…</MenuItem>
  </PromptInputSelect>;
}

export function WorktreeMenu({ task }: { task: HomeTaskLaunch }) {
  const workspace = task.project?.isGit ? "Current checkout" : "Project workspace";
  const options = [
    { value: "current", label: workspace },
    ...(task.canIsolate ? [{ value: "new", label: "New worktree", description: "Isolated" }] : []),
    ...task.trees.filter((w) => !w.isMain).map((w) => ({ value: w.id, label: w.branch || "Detached HEAD", disabled: Boolean(w.error), description: w.error ? "Unavailable" : undefined })),
  ];
  return <PromptInputSelect label="Worktree" heading="Worktree" options={options} value={task.worktree} disabled={task.busy || !task.project} onValueChange={task.chooseWorktree}
    icon={<GitBranchIcon aria-hidden />} />;
}
