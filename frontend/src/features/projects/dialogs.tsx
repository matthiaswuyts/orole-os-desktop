import { useState, type FormEvent, type ReactNode } from "react";
import type { Project } from "@/mygo";
import { branchFor } from "@/lib/projects";
import { useProjects } from "@/stores/projects";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Dialog, DialogFooter, DialogHeader, DialogPanel, DialogPopup, DialogTitle, DialogDescription } from "@/components/ui/dialog";
import { AlertDialog, AlertDialogClose, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogPopup, AlertDialogTitle } from "@/components/ui/alert-dialog";

export const errText = (e: unknown) => (e instanceof Error ? e.message : String(e)).replace(/^(bad_request|not_found|conflict|internal|denied): /, "");

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
