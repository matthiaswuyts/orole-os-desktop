import { useRef, useState } from "react";
import { MoreHorizontal, Upload } from "lucide-react";
import { Chat, type BotExport } from "@/mygo";
import { Button } from "@/components/ui/button";
import { Dialog, DialogDescription, DialogFooter, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { Menu, MenuItem, MenuPopup, MenuTrigger } from "@/components/ui/menu";
import { errText } from "@/features/projects/dialogs";
import { useChat } from "@/stores/chat";
import { parseBotExport, type ParsedExport } from "./botForm";

/** An export is a few KiB (instructions are capped at 32 KiB); anything near this size is not one. */
const MAX_IMPORT_BYTES = 1 << 20;

type Picked = { file: string; result: ParsedExport };

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

/** The New bot page's overflow menu: import a bot from an export file. */
export function BuilderMenu({ dirty, disabled }: { dirty: boolean; disabled: boolean }) {
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
          <MenuItem onClick={() => input.current?.click()}><Upload />Import from file…</MenuItem>
        </MenuPopup>
      </Menu>
      <ImportDialog picked={picked} dirty={dirty} onClose={() => setPicked(null)} />
    </>
  );
}
