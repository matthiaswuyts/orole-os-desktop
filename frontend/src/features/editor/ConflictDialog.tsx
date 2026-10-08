import { useEffect, useRef, useState } from "react";
import { EditorState } from "@codemirror/state";
import { EditorView, lineNumbers } from "@codemirror/view";
import { MergeView } from "@codemirror/merge";
import { Files } from "@/mygo";
import { decodeFile, fileError, useFiles } from "@/stores/files";
import type { EditorTab } from "@/stores/files";
import { Button } from "@/components/ui/button";
import { Dialog, DialogDescription, DialogFooter, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { editorTheme } from "./CodeEditor";

function ConflictDiff({ disk, local }: { disk: string; local: string }) {
  const host = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!host.current) return;
    const extensions = [editorTheme, lineNumbers(), EditorState.readOnly.of(true), EditorView.editable.of(false)];
    const view = new MergeView({ parent: host.current, a: { doc: disk, extensions }, b: { doc: local, extensions }, highlightChanges: true, gutter: true });
    return () => view.destroy();
  }, [disk, local]);
  return <div className="flex min-h-0 flex-col gap-2"><div className="grid grid-cols-2 text-xs text-muted-foreground"><span>On disk</span><span>Your changes</span></div><div ref={host} data-testid="file-conflict-diff" className="h-72 overflow-auto border [&_.cm-mergeView]:h-full [&_.cm-mergeViewEditors]:h-full [&_.cm-changedLine]:bg-warning/20 [&_.cm-deletedText]:bg-destructive/20 [&_.cm-insertedText]:bg-success/20" /></div>;
}

export function ConflictDialog({ tab }: { tab: EditorTab }) {
  const [disk, setDisk] = useState<string | null>(null);
  const [reading, setReading] = useState(false);
  const [error, setError] = useState("");
  const busy = tab.saving || tab.loading || reading;
  const showDiff = async () => {
    setError(""); setReading(true);
    try {
      if (tab.conflict?.deleted) { setDisk(""); return; }
      const decoded = decodeFile(await Files.read(tab.provider, tab.path, 0, 0));
      if (decoded.reason && decoded.readOnly && !decoded.text) throw new Error(decoded.reason);
      setDisk(decoded.text);
    } catch (e) { setError(fileError(e)); }
    finally { setReading(false); }
  };
  return <Dialog open={!!tab.conflict} onOpenChange={(open) => { if (!open && !busy) useFiles.getState().dismissConflict(tab.id); }}>
    <DialogPopup className={disk !== null ? "sm:max-w-4xl" : undefined}>
      <DialogHeader><DialogTitle>{tab.conflict?.deleted ? "File deleted on disk" : "File changed on disk"}</DialogTitle><DialogDescription>{tab.name} was {tab.conflict?.deleted ? "deleted" : "changed"} since you opened it. Your edits have not been saved. Reload discards your edits; overwrite replaces the disk version{tab.conflict?.deleted ? " and recreates the file" : ""}.</DialogDescription></DialogHeader>
      {(disk !== null || error || tab.error) && <DialogPanel>{disk !== null && <ConflictDiff disk={disk} local={tab.text} />}{(error || tab.error) && <p role="alert" className="mt-2 text-sm text-destructive-foreground">{error || tab.error}</p>}</DialogPanel>}
      <DialogFooter className="flex-wrap">
        <Button variant="outline" disabled={busy} onClick={() => useFiles.getState().dismissConflict(tab.id)}>Keep editing</Button>
        <Button variant="outline" disabled={busy} onClick={() => void showDiff()}>{reading ? "Reading…" : "Compare changes"}</Button>
        <Button variant="outline" disabled={busy || tab.conflict?.deleted} onClick={() => void useFiles.getState().reload(tab.id)}>Reload from disk</Button>
        <Button variant="destructive" disabled={busy || tab.readOnly} onClick={() => void useFiles.getState().save(tab.id, true)}>{tab.saving ? "Saving…" : "Overwrite disk version"}</Button>
      </DialogFooter>
    </DialogPopup>
  </Dialog>;
}
