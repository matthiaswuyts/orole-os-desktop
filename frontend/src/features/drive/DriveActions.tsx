import { useState } from "react";
import { Drive } from "@/mygo";
import type { Preview } from "@/mygo";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Field, FieldLabel } from "@/components/ui/field";
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Dialog, DialogDescription, DialogFooter, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { errorText, Failure, Loading, useRemote } from "@/features/mcp/shared";
import { useUI } from "@/stores/ui";


export function DriveActions({ file, preview, canTrash, trashed }: {
  file: { id: string; name: string };
  preview: Preview | null;
  canTrash: boolean;
  trashed: () => void;
}) {
  const recipients = useRemote(() => Drive.recipients(), "drive-share-recipients", !!preview?.snapshotHash);
  const [recipientId, setRecipientId] = useState("");
  const [task, setTask] = useState("");
  const [dialog, setDialog] = useState<"share" | "trash" | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState("");
  const recipient = recipients.data?.find((item) => item.id === recipientId);
  const eligible = !!preview?.snapshotHash && preview.fileId === file.id;

  async function confirm() {
    setBusy(true); setError(null); setNotice("");
    try {
      if (dialog === "trash") {
        const result = await Drive.trash(file.id);
        if (!result.confirmed || result.id !== file.id) throw new Error("The server did not confirm trashing this file.");
        trashed();
      } else if (dialog === "share" && preview && recipient && eligible) {
        await Drive.share({ serverId: preview.serverId, fileId: file.id, snapshotHash: preview.snapshotHash, sessionId: recipient.id, recipientRevision: recipient.revision, task });
        setNotice("Snapshot sent once. The agent or provider may retain it; no ongoing Drive access was granted.");
        recipients.reload();
      }
      setDialog(null);
    } catch (cause) {
      setError(errorText(cause));
    } finally { setBusy(false); }
  }

  return <div className="flex flex-col gap-3 border-t pt-3">
    <h3 className="text-sm font-medium">Send to an agent</h3>
    {!eligible ? <p className="text-sm text-muted-foreground">Sharing requires a complete text, Markdown, JSON or CSV preview of at most 64 KiB. Images, truncated previews and unsafe content cannot be sent.</p> : <>
      <Failure error={recipients.error} retry={recipients.reload} />
      {recipients.loading && <Loading>Loading eligible agents…</Loading>}
      {!recipients.loading && recipients.data?.length === 0 && <div className="flex flex-col items-start gap-2"><p className="text-sm text-muted-foreground">No eligible agent sessions. Start a local structured agent and wait until it is idle, or use a session that can continue. Terminal-only and busy agents cannot receive files here.</p><Button size="sm" variant="outline" onClick={() => useUI.getState().setView("agents")}>Open agents</Button></div>}
      {!!recipients.data?.length && <>
        <Field><FieldLabel>Recipient agent</FieldLabel><Select value={recipientId} onValueChange={(value) => setRecipientId(value ?? "")} disabled={busy}>
          <SelectTrigger aria-label="Recipient agent"><SelectValue placeholder="Choose an agent">{recipient ? `${recipient.title || recipient.kind} (${recipient.model || "default model"})` : undefined}</SelectValue></SelectTrigger>
          <SelectPopup>{recipients.data.map((item) => <SelectItem key={item.id} value={item.id}>{item.title || item.kind} · {item.model || "default model"} · {item.live ? "Idle" : "Continue session"}</SelectItem>)}</SelectPopup>
        </Select></Field>
        <Field><FieldLabel htmlFor="drive-share-task">What should the agent do?</FieldLabel><Textarea id="drive-share-task" value={task} onChange={(event) => setTask(event.target.value)} maxLength={4096} disabled={busy} placeholder="Describe the task for this file" /></Field>
        <p className="text-xs text-muted-foreground">Only this exact text snapshot is sent. No Drive credentials or ongoing file access are granted. Content may be retained in the agent transcript or by its provider once sent; it cannot be revoked.</p>
        <div className="flex flex-wrap gap-2"><Button size="sm" disabled={busy || !recipient || !task.trim()} onClick={() => { setError(null); setDialog("share"); }}>Review snapshot</Button><Button size="sm" variant="ghost" disabled={busy || recipients.loading} onClick={recipients.reload}>Refresh agents</Button></div>
      </>}
    </>}
    {notice && <p role="status" className="text-sm">{notice}</p>}
    {canTrash && <Button className="self-start" size="sm" variant="destructive-outline" disabled={busy} onClick={() => { setError(null); setDialog("trash"); }}>Move to trash…</Button>}
    <Dialog open={dialog !== null} onOpenChange={(open) => { if (!busy && !open) setDialog(null); }}>
      <DialogPopup><DialogHeader><DialogTitle>{dialog === "trash" ? "Move this file to trash?" : "Send this snapshot once?"}</DialogTitle><DialogDescription>{dialog === "trash" ? "This changes the selected Drive file. A separate Go-side permission decision is required before the server is called." : "Review the recipient, task and exact content. Separate sharing and agent-task permission decisions follow. Approval expires after two minutes; each attempt requires new consent."}</DialogDescription></DialogHeader>
        <DialogPanel className="flex flex-col gap-3">
          <p className="break-words text-sm font-medium">{file.name}</p>
          {dialog === "share" && preview && <><p className="break-words text-sm">Recipient: {recipient?.title || recipient?.kind} · {recipient?.model || "default model"}</p><p className="whitespace-pre-wrap break-words text-sm">Task: {task}</p><pre className="selectable max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-md border p-3 text-sm">{preview.text}</pre><p className="text-sm">The agent or provider may retain this content once sent. Expiry prevents an unsent handoff; it does not delete delivered content.</p></>}
          <Failure error={error} />
          {error && dialog === "share" && <p className="text-xs text-muted-foreground">Refresh the preview and recipients before another attempt. If delivery failed, check the agent transcript before retrying; a process may have received data before reporting an error.</p>}
          {busy && <p role="status" className="text-sm">Waiting for permission decisions and delivery…</p>}
        </DialogPanel>
        <DialogFooter><Button variant="outline" disabled={busy} onClick={() => setDialog(null)}>Cancel</Button><Button variant={dialog === "trash" ? "destructive" : "default"} disabled={busy || (dialog === "share" && (!recipient || !eligible || !task.trim() || !!error))} onClick={() => void confirm()}>{dialog === "trash" ? "Move to trash" : "Send snapshot once"}</Button></DialogFooter>
      </DialogPopup>
    </Dialog>
  </div>;
}
