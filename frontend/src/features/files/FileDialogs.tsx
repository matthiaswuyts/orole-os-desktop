import { useState } from "react";
import type { FormEvent } from "react";
import { Files } from "@/mygo";
import type { Entry } from "@/mygo";
import { fileError, joinPath, parentPath, useFiles } from "@/stores/files";
import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Dialog, DialogDescription, DialogFooter, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { ConfirmDialog } from "@/features/projects/dialogs";

export type FileMutation = { kind: "file" | "folder"; directory: string } | { kind: "rename"; entry: Entry };
export function FileNameDialog({ action, provider, close, changed }: { action: FileMutation; provider: string; close: () => void; changed: () => void }) {
  const [name, setName] = useState(action.kind === "rename" ? action.entry.name : "");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const valid = !!name.trim() && !/[\\/]/.test(name) && name !== "." && name !== "..";
  const title = action.kind === "rename" ? "Rename item" : action.kind === "folder" ? "New folder" : "New file";
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!valid || busy) return;
    setBusy(true); setError("");
    try {
      const directory = action.kind === "rename" ? parentPath(action.entry.path) : action.directory;
      const path = joinPath(directory, name);
      if (action.kind === "rename") {
        const affected = useFiles.getState().tabs.filter((t) => t.provider === provider && (t.path === action.entry.path || t.path.startsWith(`${action.entry.path}\\`) || t.path.startsWith(`${action.entry.path}/`)));
        if (affected.some((t) => t.saving || t.loading)) throw new Error("Wait for this file to finish reading or saving before renaming it.");
        const info = await Files.rename(provider, action.entry.path, path);
        useFiles.getState().renamed(provider, action.entry.path, info);
      } else if (action.kind === "folder") await Files.mkdir(provider, path);
      else {
        const result = await Files.write(provider, path, "", "", true, false);
        if (result.conflict || !result.info) throw new Error("A file already exists at this path. Choose another name.");
        await useFiles.getState().openFile(provider, path);
      }
      changed(); close();
    } catch (e) { setError(fileError(e)); }
    finally { setBusy(false); }
  };
  return <Dialog open onOpenChange={(open) => { if (!open && !busy) close(); }}><DialogPopup><form className="contents" onSubmit={(e) => void submit(e)}>
    <DialogHeader><DialogTitle>{title}</DialogTitle><DialogDescription>{action.kind === "rename" ? action.entry.path : action.directory}</DialogDescription></DialogHeader>
    <DialogPanel><Field><FieldLabel>Name</FieldLabel><Input autoFocus required value={name} onChange={(e) => setName(e.target.value)} aria-invalid={!!name && !valid} /><FieldDescription>Enter one name, without folder separators.</FieldDescription></Field>{error && <p role="alert" className="mt-2 text-xs text-destructive-foreground">{error}</p>}</DialogPanel>
    <DialogFooter><Button variant="outline" disabled={busy} onClick={close} type="button">Cancel</Button><Button type="submit" disabled={busy || !valid}>{busy ? "Working…" : action.kind === "rename" ? "Rename" : "Create"}</Button></DialogFooter>
  </form></DialogPopup></Dialog>;
}

export function DeleteFileDialog({ entry, provider, close, changed }: { entry: Entry; provider: string; close: () => void; changed: () => void }) {
  const [permanent, setPermanent] = useState(false);
  return <ConfirmDialog open onOpenChange={(open) => { if (!open) close(); }} title={`Delete ${entry.name}?`} confirmLabel={permanent ? "Delete permanently" : "Move to Recycle Bin"} extra={<label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={permanent} onChange={(e) => setPermanent(e.target.checked)} />Delete permanently instead (cannot be undone)</label>} onConfirm={async () => {
    const tabs = useFiles.getState().tabs;
    if (tabs.some((t) => t.provider === provider && (t.path === entry.path || t.path.startsWith(`${entry.path}\\`) || t.path.startsWith(`${entry.path}/`)) && (t.saving || t.loading))) throw new Error("Wait for this file to finish reading or saving before deleting it.");
    await Files.delete(provider, entry.path, permanent);
    useFiles.getState().deleted(provider, entry.path);
    changed();
  }}><p>{entry.isDir ? "The folder and its contents" : "This file"} will be {permanent ? "permanently deleted" : "moved to the Recycle Bin"}. Open editor buffers are kept so you can recover unsaved changes. If the Recycle Bin is unavailable, nothing is deleted unless you explicitly choose permanent deletion.</p></ConfirmDialog>;
}
