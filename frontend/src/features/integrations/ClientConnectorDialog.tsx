import { Button } from "@/components/ui/button";
import { Dialog, DialogDescription, DialogFooter, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Fieldset } from "@/components/ui/fieldset";
import { Input } from "@/components/ui/input";
import { Failure } from "@/features/mcp/shared";
import type { Connector } from "./directory";

/**
 * Setup for a server that does not register OAuth clients (Google): the user's own client ID and,
 * for Desktop-app clients, its secret. The secret is handed to the daemon's credential store and
 * is not shown again. Scopes are prefilled from the directory entry and editable.
 */
export function ClientConnectorDialog({ connector, busy, error, onClose, onAdd }: {
  connector: Connector | null;
  busy: boolean;
  error: string | null;
  onClose: () => void;
  onAdd: (connector: Connector, client: { id: string; secret: string; scopes: string }) => void;
}) {
  return (
    <Dialog open={connector !== null} onOpenChange={(next) => { if (!next && !busy) onClose(); }}>
      <DialogPopup anchored showCloseButton={!busy}>
        {connector?.client && (
          <>
            <DialogHeader>
              <DialogTitle>Connect {connector.name}</DialogTitle>
              <DialogDescription>{connector.client.help}</DialogDescription>
            </DialogHeader>
            <form className="contents" onSubmit={(event) => {
              event.preventDefault();
              const data = new FormData(event.currentTarget);
              onAdd(connector, { id: String(data.get("clientId")).trim(), secret: String(data.get("clientSecret")).trim(), scopes: String(data.get("scopes")).trim() });
            }}>
              <DialogPanel>
                <Fieldset className="flex flex-col gap-4" disabled={busy}>
                  <Field><FieldLabel htmlFor="client-id">Client ID</FieldLabel><Input id="client-id" name="clientId" required autoComplete="off" autoFocus /></Field>
                  <Field>
                    <FieldLabel htmlFor="client-secret">Client secret</FieldLabel>
                    <Input id="client-secret" name="clientSecret" type="password" autoComplete="off" />
                    <FieldDescription>Desktop-app clients need it for the token exchange. It is stored in Windows Credential Manager and never shown again.</FieldDescription>
                  </Field>
                  <Field><FieldLabel htmlFor="client-scopes">Scopes</FieldLabel><Input id="client-scopes" name="scopes" defaultValue={connector.client.scopes} autoComplete="off" /><FieldDescription>Space-separated. Fewer scopes means less access.</FieldDescription></Field>
                  <Failure error={error} />
                </Fieldset>
              </DialogPanel>
              <DialogFooter>
                <Button type="button" variant="outline" disabled={busy} onClick={onClose}>Cancel</Button>
                <Button type="submit" disabled={busy}>{busy ? "Connecting…" : "Connect"}</Button>
              </DialogFooter>
            </form>
          </>
        )}
      </DialogPopup>
    </Dialog>
  );
}
