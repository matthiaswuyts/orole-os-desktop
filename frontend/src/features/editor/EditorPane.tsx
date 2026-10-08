import { useEffect, useState } from "react";
import { FileCode2, LockKeyhole, Save, X } from "lucide-react";
import { useFiles, isDirty } from "@/stores/files";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { ConfirmDialog } from "@/features/projects/dialogs";
import { CodeEditor } from "./CodeEditor";
import { ConflictDialog } from "./ConflictDialog";
import { Kbd, KbdGroup } from "@/components/ui/kbd";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";

export function EditorPane() {
  const tabs = useFiles((s) => s.tabs);
  const activeId = useFiles((s) => s.activeId);
  const tab = tabs.find((t) => t.id === activeId);
  const [closing, setClosing] = useState<string | null>(null);
  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (useFiles.getState().tabs.some(isDirty)) { event.preventDefault(); event.returnValue = ""; }
    };
    window.addEventListener("beforeunload", beforeUnload);
    return () => window.removeEventListener("beforeunload", beforeUnload);
  }, []);
  useEffect(() => {
    const save = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "s") {
        event.preventDefault();
        const id = useFiles.getState().activeId;
        if (id) void useFiles.getState().save(id);
      }
    };
    window.addEventListener("keydown", save);
    return () => window.removeEventListener("keydown", save);
  }, []);
  return <section aria-label="File editor" className="flex min-h-64 min-w-0 flex-1 flex-col overflow-hidden">
    <div className="flex min-h-10 shrink-0 items-center overflow-x-auto border-b bg-muted/20" aria-label="Open files">
      {tabs.map((item) => <div key={item.id} className={cn("flex shrink-0 items-center border-r", activeId === item.id && "bg-background")}>
        <Tooltip><TooltipTrigger render={<button type="button" aria-pressed={activeId === item.id} onClick={() => useFiles.getState().activate(item.id)} className="flex h-10 items-center gap-2 px-3 text-xs outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring" />}>
          {item.readOnly ? <LockKeyhole className="size-3.5 text-muted-foreground" /> : <FileCode2 className="size-3.5 text-muted-foreground" />}
          {item.name}{isDirty(item) && <span className="size-1.5 rounded-full bg-primary" aria-label="Unsaved changes" />}
        </TooltipTrigger><TooltipPopup>{item.path}</TooltipPopup></Tooltip>
        <Button variant="ghost" size="icon-xs" className="mr-1" aria-label={`Close ${item.name}`} disabled={item.saving} onClick={() => { if (!useFiles.getState().close(item.id)) setClosing(item.id); }}><X /></Button>
      </div>)}
    </div>
    {!tab ? <Empty><EmptyHeader><EmptyTitle>No file open</EmptyTitle><EmptyDescription>Open a file from the list or search for a filename in the command palette.</EmptyDescription></EmptyHeader></Empty> : <>
      <div className="flex min-h-10 shrink-0 items-center gap-2 border-b px-3">
        <Tooltip><TooltipTrigger render={<span tabIndex={0} className="min-w-0 flex-1 truncate font-mono text-xs text-muted-foreground" />}>{tab.path}</TooltipTrigger><TooltipPopup>{tab.path}</TooltipPopup></Tooltip>
        <span role="status" className="text-xs text-muted-foreground">{tab.loading ? "Loading…" : tab.saving ? "Saving…" : tab.readOnly ? "Read-only" : isDirty(tab) ? "Unsaved" : "Saved"}</span>
        <Button size="sm" variant="outline" disabled={tab.loading || tab.readOnly || tab.saving || !isDirty(tab)} onClick={() => void useFiles.getState().save(tab.id)}><Save data-icon="inline-start" />Save</Button>
      </div>
      {tab.error && <div className="flex items-center gap-2 border-b px-3 py-2"><p role="alert" className="flex-1 text-xs text-destructive-foreground">{tab.error}</p>{!isDirty(tab) && <Button variant="outline" size="xs" disabled={tab.loading} onClick={() => void useFiles.getState().reload(tab.id)}>Retry read</Button>}</div>}
      {tab.reason && <p role="status" className="border-b px-3 py-2 text-xs text-muted-foreground">{tab.reason}</p>}
      <div className="min-h-0 flex-1">{tab.loading ? <p role="status" className="p-4 text-sm text-muted-foreground">Reading file…</p> : <CodeEditor key={tab.id} path={tab.path} text={tab.text} readOnly={tab.readOnly} onChange={(text) => useFiles.getState().edit(tab.id, text)} onSave={() => void useFiles.getState().save(tab.id)} />}</div>
      <div className="flex shrink-0 flex-wrap justify-between gap-2 border-t px-3 py-1 text-[11px] text-muted-foreground"><span>{tab.bom ? "UTF-8 with BOM" : "UTF-8"} · {tab.lineEnding === "\r\n" ? "CRLF" : "LF"}</span><span className="flex items-center gap-2"><KbdGroup aria-label="Ctrl+F"><Kbd>Ctrl</Kbd><Kbd>F</Kbd></KbdGroup> Find · <KbdGroup aria-label="Ctrl+S"><Kbd>Ctrl</Kbd><Kbd>S</Kbd></KbdGroup> Save</span></div>
      {tab.conflict && <ConflictDialog key={tab.id} tab={tab} />}
    </>}
    <ConfirmDialog open={closing !== null} onOpenChange={(open) => { if (!open) setClosing(null); }} title="Discard unsaved changes?" confirmLabel="Discard changes" onConfirm={async () => { if (closing) useFiles.getState().close(closing, true); }}>Your edits to {tabs.find((t) => t.id === closing)?.name} will be lost. Cancel to return to the editor and save them first.</ConfirmDialog>
  </section>;
}
