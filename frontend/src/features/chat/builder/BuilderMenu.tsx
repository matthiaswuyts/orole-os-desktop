import { useRef, useState } from "react";
import { Copy, Download, MoreHorizontal, Trash2, Upload } from "lucide-react";
import { Chat, type BotExport } from "@/mygo";
import { Button } from "@/components/ui/button";
import { Dialog, DialogDescription, DialogFooter, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { Menu, MenuItem, MenuPopup, MenuSeparator, MenuTrigger } from "@/components/ui/menu";
import { errText, toastError } from "@/features/projects/dialogs";
import { useChat } from "@/stores/chat";
import { parseBotExport, type ParsedExport } from "./botForm";

/** An export is a few KiB (instructions are capped at 32 KiB); anything near this size is not one. */
const MAX_IMPORT_BYTES = 1 << 20;

type Picked = { file: string; result: ParsedExport };

const slug = (name: string) => name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "") || "bot";

/** Saves the saved bot's settings as a file. Memory, messages and secrets are never part of it. */
async function exportBot(id: string) {
  try {
    const doc = await Chat.exportBot(id);
    const url = URL.createObjectURL(new Blob([JSON.stringify(doc, null, 2)], { type: "application/json" }));
    const link = document.createElement("a");
    link.href = url;
    link.download = `${slug(doc.bot.name)}.orole-bot.json`;
    link.click();
    setTimeout(() => URL.revokeObjectURL(url), 0);
  } catch (cause) {
    toastError("Could not export the bot", cause);
  }
}

function ImportDialog({ picked, dirty, onClose }: { picked: Picked | null; dirty: boolean; onClose: () => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const result = picked?.result;
  async function run(doc: BotExport) {
    setBusy(true);
    setError(null);
    try {
      const bot = await Chat.importBot(doc);
      useChat.getState().open(bot.id);
      useChat.getState().closeBuilder();
    } catch (cause) {
      setError(errText(cause));
      setBusy(false);
    }
  }
  return (
    <Dialog open={picked !== null} onOpenChange={(open) => { if (!open && !busy) { setError(null); onClose(); } }}>
      <DialogPopup>
        <DialogHeader>
          <DialogTitle>{result?.ok ? `Import “${result.value.bot.name}”?` : "This file cannot be imported"}</DialogTitle>
          <DialogDescription>{picked?.file}</DialogDescription>
        </DialogHeader>
        <DialogPanel>
          {result && !result.ok && <p role="alert" className="selectable break-words text-destructive-foreground text-sm">{result.error}</p>}
          {result?.ok && (
            <div className="flex flex-col gap-2 text-sm">
              <p>This creates a new bot from the file’s settings and instructions. Memory, messages and secrets are never part of an export.</p>
              {dirty && <p className="text-warning-foreground">The unsaved changes in this builder are discarded.</p>}
            </div>
          )}
          {error && <p role="alert" className="selectable mt-2 break-words text-destructive-foreground text-sm">{error}</p>}
        </DialogPanel>
        <DialogFooter>
          <Button variant="outline" disabled={busy} onClick={onClose}>{result?.ok ? "Cancel" : "Close"}</Button>
          {result?.ok && <Button disabled={busy} onClick={() => void run(result.value)}>Import bot</Button>}
        </DialogFooter>
      </DialogPopup>
    </Dialog>
  );
}

/** Duplicate, export, import and archive: the actions that are not part of editing one bot's settings. */
export function BuilderMenu({ botId, dirty, disabled, onDuplicate, onArchive }: {
  botId: string | null; dirty: boolean; disabled: boolean; onDuplicate: () => void; onArchive: () => void;
}) {
  const input = useRef<HTMLInputElement>(null);
  const [picked, setPicked] = useState<Picked | null>(null);

  async function choose(file: File | undefined) {
    if (input.current) input.current.value = "";
    if (!file) return;
    if (file.size > MAX_IMPORT_BYTES) { setPicked({ file: file.name, result: { ok: false, error: "This file is too large to be a bot export." } }); return; }
    setPicked({ file: file.name, result: parseBotExport(await file.text()) });
  }

  return (
    <>
      <input ref={input} type="file" accept=".json,application/json" hidden aria-label="Choose a bot export file" onChange={(e) => void choose(e.target.files?.[0])} />
      <Menu>
        <MenuTrigger render={<Button size="icon-sm" variant="ghost" aria-label="More actions" disabled={disabled} />}><MoreHorizontal /></MenuTrigger>
        <MenuPopup align="end">
          {botId && <MenuItem onClick={onDuplicate}><Copy />Duplicate{dirty ? " (with unsaved changes)" : ""}</MenuItem>}
          {botId && <MenuItem onClick={() => void exportBot(botId)}><Download />Export{dirty ? " (saved version)" : ""}</MenuItem>}
          <MenuItem onClick={() => input.current?.click()}><Upload />Import from file…</MenuItem>
          {botId && <MenuSeparator />}
          {botId && <MenuItem variant="destructive" onClick={onArchive}><Trash2 />Archive…</MenuItem>}
        </MenuPopup>
      </Menu>
      <ImportDialog picked={picked} dirty={dirty} onClose={() => setPicked(null)} />
    </>
  );
}
