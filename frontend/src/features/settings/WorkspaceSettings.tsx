import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field, FieldLabel } from "@/components/ui/field";
import { ConfirmDialog } from "@/features/projects/dialogs";
import { useWorkspaces } from "@/stores/workspaces";
import { inDesktop } from "@/lib/backend";

export function WorkspaceSettings() {
  const { list, activeId, loaded, error: saveError, create, rename, remove, select } = useWorkspaces();
  const [name, setName] = useState("");
  const [editing, setEditing] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const [deleting, setDeleting] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  async function save(action: () => Promise<void>) {
    if (busy) return;
    setBusy(true); setError(null);
    try { await action(); }
    catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
    finally { setBusy(false); }
  }
  const removed = list.find((workspace) => workspace.id === deleting);
  return <section aria-label="Workspace management" className="max-w-2xl space-y-4">
    <p className="text-muted-foreground">Each workspace has its own dashboard widgets and layout. Changing workspaces does not stop agents or terminals.</p>
    {!inDesktop && <p className="text-xs text-muted-foreground">Preview workspaces are stored only in this browser.</p>}
    <form className="flex items-end gap-2" onSubmit={(event) => { event.preventDefault(); if (name.trim()) void save(async () => { await create(name.trim()); setName(""); }); }}>
      <Field className="flex-1"><FieldLabel>New workspace name</FieldLabel><Input value={name} maxLength={100} disabled={busy || !loaded} onChange={(event) => setName(event.target.value)} /></Field>
      <Button type="submit" disabled={busy || !loaded || !name.trim()}>Create workspace</Button>
    </form>
    {(error || saveError) && <p role="alert" className="text-destructive-foreground">{error || saveError}</p>}
    <ul className="divide-y rounded-lg border bg-card">
      {list.map((workspace) => <li key={workspace.id} className="flex flex-wrap items-center gap-2 p-3">
        {editing === workspace.id ? <form className="flex flex-1 items-center gap-2" onSubmit={(event) => { event.preventDefault(); if (draft.trim()) void save(async () => { await rename(workspace.id, draft); setEditing(null); }); }}>
          <Input aria-label="Workspace name" autoFocus maxLength={100} value={draft} disabled={busy} onChange={(event) => setDraft(event.target.value)} />
          <Button size="sm" type="submit" disabled={busy || !draft.trim()}>Save name</Button>
          <Button size="sm" type="button" variant="ghost" disabled={busy} onClick={() => setEditing(null)}>Cancel</Button>
        </form> : <>
          <span className="min-w-0 flex-1 truncate">{workspace.name}{workspace.id === activeId && <span className="ml-2 text-xs text-muted-foreground">Current</span>}</span>
          <Button size="sm" variant="ghost" disabled={busy || workspace.id === activeId} onClick={() => select(workspace.id)} aria-label={`Switch to ${workspace.name}`}>Switch</Button>
          <Button size="sm" variant="outline" disabled={busy} onClick={() => { setEditing(workspace.id); setDraft(workspace.name); }} aria-label={`Rename ${workspace.name}`}>Rename</Button>
          <Button size="sm" variant="ghost" disabled={busy || list.length <= 1} onClick={() => setDeleting(workspace.id)} aria-label={`Delete ${workspace.name}`}>Delete</Button>
        </>}
      </li>)}
    </ul>
    <p className="text-xs text-muted-foreground">Keep at least one workspace. Deleting a workspace removes its dashboard layout, not projects or files.</p>
    <ConfirmDialog open={!!removed} onOpenChange={(open) => { if (!open) setDeleting(null); }} title={`Delete ${removed?.name ?? "workspace"}?`} confirmLabel="Delete workspace" onConfirm={async () => { if (deleting) await remove(deleting); }}>
      This removes the saved dashboard and widget configuration. Projects, files, agents and terminals are unchanged.
    </ConfirmDialog>
  </section>;
}
