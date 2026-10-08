import { useState } from "react";
import type { Project } from "@/mygo";
import { useProjects } from "@/stores/projects";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { errText } from "./dialogs";

/** Where new worktrees of a project are created. The daemon validates the path. */
export function WorktreeRootField({ project }: { project: Project }) {
  const set = useProjects((s) => s.setWorktreeRoot);
  const [value, setValue] = useState(project.worktreeRootCustom ? project.worktreeRoot : "");
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const save = (root: string) => { setBusy(true); setErr(null); set(project.id, root).then(() => setValue(root), (e) => setErr(errText(e))).finally(() => setBusy(false)); };
  const dirty = value.trim() !== (project.worktreeRootCustom ? project.worktreeRoot : "");
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex gap-2">
        <Input aria-label="Worktree root" value={value} onChange={(e) => setValue(e.target.value)} placeholder={project.worktreeRoot} className="font-mono" />
        <Button size="sm" variant="outline" disabled={busy || !dirty} onClick={() => save(value.trim())}>Save</Button>
        {project.worktreeRootCustom && <Button size="sm" variant="ghost" disabled={busy} onClick={() => save("")}>Reset</Button>}
      </div>
      <p className="text-muted-foreground text-xs">{project.worktreeRootCustom ? "Custom location." : "Default location: next to the project in .orole-worktrees."} Worktrees are created as <code className="font-mono">&lt;root&gt;/&lt;name&gt;</code> on branch <code className="font-mono">orole/&lt;name&gt;</code>.</p>
      {err && <p role="alert" className="text-destructive-foreground text-sm">{err}</p>}
    </div>
  );
}
