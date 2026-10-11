import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogDescription, DialogFooter, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { Field, FieldLabel } from "@/components/ui/field";
import { Fieldset } from "@/components/ui/fieldset";
import { Input } from "@/components/ui/input";
import { Failure } from "@/features/mcp/shared";
import { nameFromUrl } from "./directory";

/**
 * Name and server URL, nothing else. Sign-in is detected after adding: a server that asks for
 * OAuth opens the browser, an open one just connects. Pre-registered client IDs and scopes live
 * under Manage for the few servers that need them.
 */
export function CustomConnectorDialog({ open, busy, error, onClose, onAdd }: {
  open: boolean;
  busy: boolean;
  error: string | null;
  onClose: () => void;
  onAdd: (name: string, url: string) => void;
}) {
  const [url, setUrl] = useState("");
  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next && !busy) onClose(); }}>
      <DialogPopup anchored showCloseButton={!busy}>
        <DialogHeader>
          <DialogTitle>Custom connector</DialogTitle>
          <DialogDescription>Enter a name and an MCP server URL.</DialogDescription>
        </DialogHeader>
        <form className="contents" onSubmit={(event) => {
          event.preventDefault();
          const data = new FormData(event.currentTarget);
          const address = String(data.get("url")).trim();
          onAdd(String(data.get("name")).trim() || nameFromUrl(address) || address, address);
        }}>
          <DialogPanel>
            <Fieldset className="flex flex-col gap-4" disabled={busy}>
              <Field><FieldLabel htmlFor="connector-name">Name</FieldLabel><Input id="connector-name" name="name" placeholder={nameFromUrl(url) || "My Connector"} autoComplete="off" /></Field>
              <Field><FieldLabel htmlFor="connector-url">Server URL</FieldLabel><Input id="connector-url" name="url" type="url" required placeholder="https://mcp.example.com/mcp" autoComplete="off" autoFocus value={url} onChange={(event) => setUrl(event.target.value)} /></Field>
              <Failure error={error} />
            </Fieldset>
          </DialogPanel>
          <DialogFooter>
            <Button type="button" variant="outline" disabled={busy} onClick={onClose}>Cancel</Button>
            <Button type="submit" disabled={busy || !url.trim()}>{busy ? "Adding…" : "Add connector"}</Button>
          </DialogFooter>
        </form>
      </DialogPopup>
    </Dialog>
  );
}
