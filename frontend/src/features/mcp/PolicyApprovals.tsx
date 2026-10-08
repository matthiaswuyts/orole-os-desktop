import { useEffect, useState } from "react";
import { events, Policy, type Pending } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Field, FieldLabel } from "@/components/ui/field";
import { Dialog, DialogDescription, DialogFooter, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { errorText, Failure } from "./shared";

/** Global daemon approval queue: subscribe before fetching so an initial snapshot cannot hide a new request. */
export function PolicyApprovals() {
  const [pending, setPending] = useState<Pending[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    if (!inDesktop) return;
    let live = true;
    let sequence = 0;
    const refresh = () => {
      const version = ++sequence;
      Policy.pending().then((items) => { if (live && version === sequence) { setPending(items ?? []); setError(null); } }, (cause: unknown) => { if (live && version === sequence) setError(errorText(cause)); });
    };
    const offPending = events.policyPending.on(({ items }) => { sequence++; setPending(items ?? []); setError(null); });
    const offDaemon = events.daemonState.on((state) => { if (state.connected) refresh(); else { sequence++; setPending([]); } });
    refresh();
    return () => { live = false; offPending(); offDaemon(); };
  }, [revision]);
  const request = pending[0];
  if (!request) return error ? <div className="fixed right-4 bottom-4 max-w-sm"><Failure error={`Approval queue unavailable: ${error}`} retry={() => setRevision((value) => value + 1)} /></div> : null;
  return <Approval key={request.id} request={request} count={pending.length} resolved={() => { setPending((items) => items.filter((item) => item.id !== request.id)); setRevision((value) => value + 1); }} />;
}

function Approval({ request, count, resolved }: { request: Pending; count: number; resolved: () => void }) {
  const [remember, setRemember] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [now, setNow] = useState(Date.now());
  useEffect(() => { const timer = window.setInterval(() => setNow(Date.now()), 1000); return () => window.clearInterval(timer); }, []);
  const expired = now >= request.expiresAt;
  async function resolve(allow: boolean) {
    setBusy(true); setError(null);
    try { await Policy.resolve(request.id, allow, request.canRemember && remember); resolved(); }
    catch (cause) { setError(errorText(cause)); }
    finally { setBusy(false); }
  }
  return <Dialog open disablePointerDismissal>
    <DialogPopup showCloseButton={false} anchored>
      <DialogHeader><DialogTitle>Approve this action?</DialogTitle><DialogDescription>{count > 1 ? `${count} actions are waiting. ` : ""}Only approve actions you recognize. Descriptions can contain untrusted external content.</DialogDescription></DialogHeader>
      <DialogPanel><div className="flex flex-col gap-4">
        <Badge variant={request.effect === "destructive" ? "error" : "warning"}>{request.effect || "Unclassified action"}</Badge>
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm"><dt className="text-muted-foreground">Requested by</dt><dd className="break-all">{request.actor}</dd><dt className="text-muted-foreground">Action</dt><dd className="break-all">{request.action}</dd><dt className="text-muted-foreground">Resource</dt><dd className="break-all">{request.resource}</dd></dl>
        <p className="selectable whitespace-pre-wrap break-words rounded-md bg-muted p-3 text-sm">{request.detail || "No additional details supplied."}</p>
        <p className="text-xs text-muted-foreground">{request.reason}{request.fromHint ? " The effect is based on untrusted server annotations." : ""}</p>
        {request.canRemember && <Field><FieldLabel><Checkbox checked={remember} disabled={busy || expired} onCheckedChange={(checked) => setRemember(checked === true)} />Remember for this exact actor, action and resource</FieldLabel></Field>}
        <p role="status" className="text-xs text-muted-foreground">{expired ? "This approval has expired. It cannot authorize the action." : `Expires at ${new Date(request.expiresAt).toLocaleTimeString()}`}</p>
        <Failure error={error} />
      </div></DialogPanel>
      <DialogFooter>{expired ? <Button onClick={resolved}>Dismiss expired request</Button> : <><Button variant="outline" disabled={busy} onClick={() => void resolve(false)}>Deny</Button><Button variant={request.effect === "destructive" ? "destructive" : "default"} disabled={busy} onClick={() => void resolve(true)}>{busy ? "Resolving…" : "Allow"}</Button></>}</DialogFooter>
    </DialogPopup>
  </Dialog>;
}
