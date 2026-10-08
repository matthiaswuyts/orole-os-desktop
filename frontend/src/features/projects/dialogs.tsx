import { useState, type FormEvent, type ReactNode } from "react";
import { FolderOpen, X } from "lucide-react";
import { Projects, type Project } from "@/mygo";
import { branchFor } from "@/lib/projects";
import { useProjects } from "@/stores/projects";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Dialog, DialogFooter, DialogHeader, DialogPanel, DialogPopup, DialogTitle, DialogDescription } from "@/components/ui/dialog";
import { AlertDialog, AlertDialogClose, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogPopup, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { toastManager } from "@/components/ui/toast";

export const errText = (e: unknown) => (e instanceof Error ? e.message : String(e)).replace(/^(bad_request|not_found|conflict|internal|denied): /, "");

export const toastError = (title: string, e: unknown) => toastManager.add({ type: "error", title, description: errText(e), timeout: 8000 });

/** Confirmation for destructive actions. `onConfirm` may reject; the message is shown inline. */
export function ConfirmDialog({ open, onOpenChange, title, children, confirmLabel, onConfirm, extra }: {
  open: boolean; onOpenChange: (o: boolean) => void; title: string; children: ReactNode; confirmLabel: string; onConfirm: () => Promise<void>; extra?: ReactNode;
}) {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  return (
    <AlertDialog open={open} onOpenChange={(o) => { if (!busy) { onOpenChange(o); if (!o) setErr(null); } }}>
      <AlertDialogPopup>
        <AlertDialogHeader><AlertDialogTitle>{title}</AlertDialogTitle><AlertDialogDescription render={<div />}>{children}</AlertDialogDescription></AlertDialogHeader>
        {(extra || err) && <div className="flex flex-col gap-2 px-6 pb-4">{extra}{err && <p role="alert" className="text-destructive-foreground text-sm">{err}</p>}</div>}
        <AlertDialogFooter>
          <AlertDialogClose render={<Button variant="outline" disabled={busy} />}>Cancel</AlertDialogClose>
          <Button variant="destructive" disabled={busy} onClick={() => { setBusy(true); setErr(null); onConfirm().then(() => onOpenChange(false), (e) => setErr(errText(e))).finally(() => setBusy(false)); }}>{confirmLabel}</Button>
        </AlertDialogFooter>
      </AlertDialogPopup>
    </AlertDialog>
  );
}

/** Last path segment of a native path, for suggesting a project name from a folder. */
const folderName = (p: string) => p.replace(/[\\/]+$/, "").split(/[\\/]/).pop() ?? "";

/** Creates a project: a name plus an optional folder (any folder; a git repository is not required). */
export function NewProjectDialog() {
  const open = useProjects((s) => s.newProjectOpen);
  const setOpen = useProjects((s) => s.setNewProjectOpen);
  const create = useProjects((s) => s.create);
  const [name, setName] = useState("");
  const [path, setPath] = useState("");
  const [busy, setBusy] = useState(false);
  const reset = () => { setName(""); setPath(""); };
  const close = (o: boolean) => { if (busy) return; setOpen(o); if (!o) reset(); };
  const browse = () => Projects.pickFolder().then((p) => { if (p) setPath(p); }, (e) => toastError("Could not open the folder picker", e));
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (busy || (!name.trim() && !path)) return;
    setBusy(true);
    create(name, path).then((project) => { useProjects.getState().select(project.id); setOpen(false); reset(); }, (x) => toastError("Could not create project", x)).finally(() => setBusy(false));
  };
  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogPopup>
        <form onSubmit={submit} className="contents">
          <DialogHeader><DialogTitle>New project</DialogTitle><DialogDescription>Give the project a name. A folder is optional; it does not need to be a git repository.</DialogDescription></DialogHeader>
          <DialogPanel className="flex flex-col gap-3">
            <Field><FieldLabel>Name</FieldLabel><Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder={path ? folderName(path) : "My project"} />
              {path && <FieldDescription>Leave empty to use the folder name.</FieldDescription>}</Field>
            <Field><FieldLabel>Folder <span className="font-normal text-muted-foreground">(optional)</span></FieldLabel>
              <div className="flex gap-2">
                <Input readOnly value={path} placeholder="No folder" className="font-mono" aria-label="Folder" />
                <Button type="button" variant="outline" onClick={() => void browse()}><FolderOpen />Browse…</Button>
                {path && <Button type="button" variant="outline" size="icon" aria-label="Clear folder" onClick={() => setPath("")}><X /></Button>}
              </div>
              <FieldDescription>{path ? "Orole OS reads this folder in place; nothing is copied or changed." : "Without a folder the project has no files, git state or worktrees."}</FieldDescription></Field>
          </DialogPanel>
          <DialogFooter>
            <Button type="button" variant="outline" disabled={busy} onClick={() => close(false)}>Cancel</Button>
            <Button type="submit" disabled={busy || (!name.trim() && !path)}>{busy ? "Creating…" : "Create project"}</Button>
          </DialogFooter>
        </form>
      </DialogPopup>
    </Dialog>
  );
}

/** Creates a worktree on a new `orole/<slug>` branch. */
export function CreateWorktreeDialog({ project, open, onOpenChange }: { project: Project | undefined; open: boolean; onOpenChange: (o: boolean) => void }) {
  const create = useProjects((s) => s.createWorktree);
  const [name, setName] = useState("");
  const [base, setBase] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const branch = branchFor(name);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!project || !branch) return;
    setBusy(true); setErr(null);
    create(project.id, name, base.trim()).then(() => { setName(""); setBase(""); onOpenChange(false); }, (x) => setErr(errText(x))).finally(() => setBusy(false));
  };
  return (
    <Dialog open={open} onOpenChange={(o) => { if (!busy) onOpenChange(o); }}>
      <DialogPopup>
        <form onSubmit={submit} className="contents">
          <DialogHeader><DialogTitle>New worktree{project ? ` in ${project.name}` : ""}</DialogTitle><DialogDescription>Creates a separate checkout on its own branch so work does not touch your main checkout.</DialogDescription></DialogHeader>
          <DialogPanel className="flex flex-col gap-3">
            <Field><FieldLabel>Name</FieldLabel><Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="fix login redirect" />
              <FieldDescription>{branch ? <>Branch <code className="font-mono">{branch}</code> in <code className="font-mono break-all">{project?.worktreeRoot}</code></> : "Use letters or digits."}</FieldDescription></Field>
            <Field><FieldLabel>Start from</FieldLabel><Input value={base} onChange={(e) => setBase(e.target.value)} placeholder="HEAD (current commit)" className="font-mono" />
              <FieldDescription>A branch, tag or commit. Leave empty for HEAD.</FieldDescription></Field>
            {err && <p role="alert" className="text-destructive-foreground text-sm">{err}</p>}
          </DialogPanel>
          <DialogFooter>
            <Button type="button" variant="outline" disabled={busy} onClick={() => onOpenChange(false)}>Cancel</Button>
            <Button type="submit" disabled={busy || !branch || !project}>{busy ? "Creating…" : "Create worktree"}</Button>
          </DialogFooter>
        </form>
      </DialogPopup>
    </Dialog>
  );
}
