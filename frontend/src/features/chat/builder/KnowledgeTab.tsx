import { useEffect, useRef, useState, type Dispatch } from "react";
import { FileText, Trash2, Upload } from "lucide-react";
import { Chat, events, type KnowledgeFile } from "@/mygo";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmDialog, errText } from "@/features/projects/dialogs";
import { formatSize } from "../parts/format";
import { TOOL_IDS, emptyToolSettings, type Action, type FormState, type TabId } from "./botForm";
import { Section } from "./controls";
import { uploadKnowledge } from "./upload";
import { useLoad } from "./useLoad";

const POLL_MS = 1500;

interface Transfer {
  key: number;
  name: string;
  size: number;
  sent: number;
  error: string | null;
}

const STATUS: Record<string, { label: string; variant: "success" | "warning" | "error" | "secondary" }> = {
  ready: { label: "Ready", variant: "success" },
  indexing: { label: "Indexing", variant: "warning" },
  error: { label: "Failed", variant: "error" },
};

/** Documents the bot can search. Stored and indexed on this computer by the daemon. */
export function KnowledgeTab({ botId, form, dispatch, onTab }: { botId: string | null; form: FormState; dispatch: Dispatch<Action>; onTab: (tab: TabId) => void }) {
  const files = useLoad(() => Chat.knowledgeFiles(botId ?? ""), botId ?? "", botId !== null);
  const [transfers, setTransfers] = useState<Transfer[]>([]);
  const [removing, setRemoving] = useState<KnowledgeFile | null>(null);
  const input = useRef<HTMLInputElement>(null);
  const counter = useRef(0);
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const { reload } = files;
  useEffect(() => events.chatMemory.on(() => reload()), [reload]);
  const indexing = (files.data ?? []).some((f) => f.status === "indexing");
  useEffect(() => {
    if (!indexing) return;
    const timer = setTimeout(reload, POLL_MS);
    return () => clearTimeout(timer);
  }, [indexing, files.data, reload]);

  if (!botId) {
    return (
      <Section title="Knowledge" description="Documents the bot can look things up in.">
        <p role="status" className="text-muted-foreground text-sm">Save the bot first. Then you can attach documents to it here.</p>
      </Section>
    );
  }
  const id = botId;

  async function add(list: FileList | null) {
    const picked = Array.from(list ?? []);
    if (input.current) input.current.value = "";
    for (const file of picked) {
      const key = ++counter.current;
      const update = (patch: Partial<Transfer>) => { if (mounted.current) setTransfers((all) => all.map((t) => (t.key === key ? { ...t, ...patch } : t))); };
      setTransfers((all) => [...all, { key, name: file.name, size: file.size, sent: 0, error: null }]);
      try {
        await uploadKnowledge(id, file, (sent) => update({ sent }));
        if (mounted.current) setTransfers((all) => all.filter((t) => t.key !== key));
      } catch (cause) {
        update({ error: errText(cause) });
      }
      if (mounted.current) reload();
    }
  }

  const list = files.data ?? [];
  const searchOn = TOOL_IDS.knowledge in form.tools;
  return (
    <div className="flex flex-col gap-3">
      <Section title="Knowledge files" description="Text documents the bot can search. They are stored and indexed on this computer. Passages the bot finds are sent to the model provider as part of the conversation.">
        {!searchOn && (
          <Alert variant="warning">
            <AlertDescription className="flex flex-wrap items-center justify-between gap-2">
              <span>The bot only reads these files when the Knowledge search tool is on.</span>
              <span className="flex gap-1.5">
                <Button size="xs" variant="outline" onClick={() => dispatch({ type: "tool", id: TOOL_IDS.knowledge, settings: emptyToolSettings() })}>Turn it on</Button>
                <Button size="xs" variant="ghost" onClick={() => onTab("tools")}>Open Tools</Button>
              </span>
            </AlertDescription>
          </Alert>
        )}
        <div className="flex flex-wrap items-center gap-2">
          <input ref={input} type="file" multiple hidden aria-label="Choose knowledge files" onChange={(e) => void add(e.target.files)} />
          <Button size="sm" variant="outline" onClick={() => input.current?.click()}><Upload />Add files</Button>
          <span className="text-muted-foreground text-xs">Text files only, up to 20 MiB each and 10 per bot. PDFs and images cannot be indexed yet.</span>
        </div>
        {files.error && (
          <Alert variant="error"><AlertDescription className="flex items-center justify-between gap-2"><span className="selectable break-words">Could not load the files: {files.error}</span><Button size="xs" variant="outline" onClick={reload}>Try again</Button></AlertDescription></Alert>
        )}
        {transfers.length > 0 && (
          <ul aria-label="Uploads" className="flex flex-col gap-1.5">
            {transfers.map((t) => (
              <li key={t.key} className="flex min-w-0 flex-col gap-0.5 rounded-md border bg-background/40 p-2 text-xs">
                <span className="flex items-center justify-between gap-2">
                  <span className="min-w-0 break-words font-medium">{t.name}</span>
                  {t.error ? <Button size="xs" variant="ghost" onClick={() => setTransfers((all) => all.filter((x) => x.key !== t.key))}>Dismiss</Button> : <span className="text-muted-foreground tabular-nums" role="status">Uploading {t.size ? Math.round((t.sent / t.size) * 100) : 0}%</span>}
                </span>
                {t.error && <span role="alert" className="selectable break-words text-destructive-foreground">{t.error}</span>}
              </li>
            ))}
          </ul>
        )}
        {files.loading && !files.data && <p role="status" className="text-muted-foreground text-xs">Loading files…</p>}
        {files.data && list.length === 0 && transfers.length === 0 && <p className="text-muted-foreground text-xs">No files yet.</p>}
        {list.length > 0 && (
          <ul aria-label="Knowledge files" className="flex flex-col gap-1.5">
            {list.map((file) => {
              const status = STATUS[file.status] ?? { label: file.status, variant: "secondary" as const };
              return (
                <li key={file.id} className="flex min-w-0 items-start justify-between gap-2 rounded-md border bg-background/40 p-2.5">
                  <div className="flex min-w-0 flex-col gap-1">
                    <span className="flex items-center gap-1.5 text-sm"><FileText className="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" /><span className="selectable min-w-0 break-words font-medium">{file.name}</span></span>
                    <span className="flex flex-wrap items-center gap-1.5 text-muted-foreground text-xs">
                      <Badge size="sm" variant={status.variant}>{status.label}</Badge>
                      <span>{formatSize(file.size)}</span>
                      {file.status === "ready" && <span>{file.chunks} {file.chunks === 1 ? "passage" : "passages"}</span>}
                    </span>
                    {file.error && <span role={file.status === "error" ? "alert" : undefined} className={file.status === "error" ? "selectable break-words text-destructive-foreground text-xs" : "selectable break-words text-muted-foreground text-xs"}>{file.error}</span>}
                  </div>
                  <Button size="icon-sm" variant="ghost" aria-label={`Remove ${file.name}`} onClick={() => setRemoving(file)}><Trash2 /></Button>
                </li>
              );
            })}
          </ul>
        )}
      </Section>
      <ConfirmDialog
        open={removing !== null} onOpenChange={(o) => { if (!o) setRemoving(null); }}
        title={`Remove “${removing?.name ?? ""}”?`} confirmLabel="Remove file"
        onConfirm={async () => {
          if (!removing) return;
          await Chat.removeKnowledge({ botId: id, attachmentId: removing.id });
          reload();
        }}
      >
        <p>The bot can no longer look things up in this file. Your original is not touched.</p>
      </ConfirmDialog>
    </div>
  );
}
