import { useState } from "react";
import { Plus } from "lucide-react";
import type { ManifestRecord } from "@/mygo";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { AlertDialog, AlertDialogClose, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogPopup, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogDescription, DialogFooter, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { Field, FieldLabel } from "@/components/ui/field";
import { Failure } from "@/features/mcp/shared";
import { EditManifestDialog, ImportProviderDialog, ManifestSummary } from "@/features/settings/ProviderManifestDialogs";
import { useLLM } from "@/stores/llm";

/** Trusted means enabled for exactly this digest; the daemon guarantees the digest match, this only mirrors it. */
const isTrusted = (record: ManifestRecord) => record.enabled && record.digest !== "" && record.digest === record.approvedDigest;

const shortDigest = (digest: string) => digest.slice(0, 12);

function hostOf(record: ManifestRecord): string {
  try { return new URL(record.manifest.baseUrl).host; } catch { return record.manifest.baseUrl; }
}

/** What a failed trust attempt means for the user, in addition to the daemon's own message. */
function trustFailure(code: string | undefined, error: string): string {
  if (code === "denied") return `The approval request was denied, so the provider stays disabled. ${error}`;
  if (code === "canceled") return `The approval request was canceled (the manifest changed, was revoked or removed, or the daemon stopped). Nothing was enabled. ${error}`;
  if (code === "conflict") return `The manifest changed since you opened this review. Close it and review the current version. ${error}`;
  return error;
}

function TrustDialog({ record, onClose }: { record: ManifestRecord; onClose: () => void }) {
  const { id } = record.manifest;
  const pending = useLLM((state) => state.manifestBusy[id] === "trust");
  const live = useLLM((state) => state.manifests.find((r) => r.manifest.id === id));
  const [reviewed, setReviewed] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Trust is pinned to the digest that was reviewed; if the stored manifest moved on, this review no longer applies.
  const stale = !live || live.digest !== record.digest;

  async function trust() {
    if (pending || stale) return;
    setError(null);
    const result = await useLLM.getState().trustManifest(id, record.digest, true);
    if (result.ok) onClose();
    else setError(trustFailure(result.code, result.error));
  }

  return (
    <Dialog open onOpenChange={(open) => { if (!open && !pending) onClose(); }}>
      <DialogPopup>
        <DialogHeader>
          <DialogTitle>Review {record.manifest.name}</DialogTitle>
          <DialogDescription>
            Trust applies to this exact manifest. After you confirm, a separate approval request from the daemon appears; the provider is only enabled once you approve it there.
          </DialogDescription>
        </DialogHeader>
        <DialogPanel className="flex flex-col gap-4">
          <ManifestSummary manifest={record.manifest} />
          <details>
            <summary className="min-h-6 cursor-pointer rounded-sm text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring">Complete manifest</summary>
            <pre aria-label="Complete manifest" className="selectable mt-2 max-h-64 overflow-auto rounded-lg border bg-muted/40 p-3 font-mono text-xs">{JSON.stringify(record.manifest, null, 2)}</pre>
          </details>
          <p className="selectable break-all font-mono text-xs text-muted-foreground">SHA-256 {record.digest}</p>
          <Field>
            <FieldLabel htmlFor={`manifest-reviewed-${id}`}>
              <input id={`manifest-reviewed-${id}`} type="checkbox" checked={reviewed} disabled={pending || stale} onChange={(event) => setReviewed(event.target.checked)} className="size-4 shrink-0 accent-primary" />
              I reviewed the host, auth and headers
            </FieldLabel>
          </Field>
          {stale && (
            <Alert variant="warning">
              <AlertTitle>This manifest changed</AlertTitle>
              <AlertDescription>It was changed or removed since you opened this review. Close this and review the current version.</AlertDescription>
            </Alert>
          )}
          {pending && <p role="status" className="text-sm text-muted-foreground">Waiting for your approval: the daemon is showing a separate approval request. Approve or deny it there.</p>}
          {error && (
            <Alert variant="error">
              <AlertTitle>{error.startsWith("The approval request was denied") ? "Trust denied" : "Could not enable the provider"}</AlertTitle>
              <AlertDescription className="selectable break-words">{error}</AlertDescription>
            </Alert>
          )}
        </DialogPanel>
        <DialogFooter>
          <Button type="button" variant="outline" disabled={pending} onClick={onClose}>Cancel</Button>
          <Button type="button" loading={pending} disabled={!reviewed || stale} onClick={() => void trust()}>Trust and enable</Button>
        </DialogFooter>
      </DialogPopup>
    </Dialog>
  );
}

function ManifestRow({ record }: { record: ManifestRecord }) {
  const { id, name } = record.manifest;
  const busy = useLLM((state) => state.manifestBusy[id]);
  // The dialogs keep the record the user opened: trust and removal are pinned to that exact digest.
  const [dialog, setDialog] = useState<{ kind: "trust" | "edit" | "remove"; record: ManifestRecord } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const trusted = isTrusted(record);

  async function revoke() {
    setError(null);
    const result = await useLLM.getState().trustManifest(id, record.digest, false);
    if (!result.ok) setError(result.error);
  }

  async function remove() {
    if (dialog?.kind !== "remove") return;
    setError(null);
    const result = await useLLM.getState().removeManifest(id, dialog.record.digest);
    if (result.ok) setDialog(null);
    else setError(result.error);
  }

  return (
    <li aria-label={`${name} manifest`} className="flex min-w-0 flex-col gap-2 p-3">
      <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2">
        <div className="flex min-w-0 flex-1 flex-col">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <h3 className="min-w-0 break-words font-medium">{name}</h3>
            <Badge variant={trusted ? "success" : "outline"}>{trusted ? "Trusted and enabled" : "Disabled / needs review"}</Badge>
          </div>
          <p className="selectable break-all font-mono text-xs text-muted-foreground">{hostOf(record)} · {shortDigest(record.digest)}</p>
        </div>
        <div className="flex shrink-0 flex-wrap items-center gap-1.5">
          {trusted
            ? <Button size="sm" variant="outline" loading={busy === "revoke"} disabled={!!busy} onClick={() => void revoke()}>Revoke</Button>
            : <Button size="sm" disabled={!!busy} onClick={() => { setError(null); setDialog({ kind: "trust", record }); }}>Review &amp; trust</Button>}
          <Button size="sm" variant="ghost" disabled={!!busy} onClick={() => { setError(null); setDialog({ kind: "edit", record }); }}>Edit JSON</Button>
          <Button size="sm" variant="destructive-outline" disabled={!!busy} onClick={() => { setError(null); setDialog({ kind: "remove", record }); }}>Remove</Button>
        </div>
      </div>
      {error && dialog?.kind !== "remove" && <p role="alert" className="selectable break-words text-xs text-destructive-foreground">{error}</p>}
      {dialog?.kind === "trust" && <TrustDialog record={dialog.record} onClose={() => setDialog(null)} />}
      {dialog?.kind === "edit" && <EditManifestDialog id={id} digest={dialog.record.digest} manifest={dialog.record.manifest} onClose={() => setDialog(null)} />}
      <AlertDialog open={dialog?.kind === "remove"} onOpenChange={(open) => { if (!open && busy !== "remove") setDialog(null); }}>
        <AlertDialogPopup>
          <AlertDialogHeader>
            <AlertDialogTitle>Remove {name}?</AlertDialogTitle>
            <AlertDialogDescription render={<div />}>
              This deletes the manifest and the API key stored for it in Windows Credential Manager. Bots that use {name} models stop answering. The key itself stays valid on the provider's side; revoke it there if you want it gone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          {error && <p role="alert" className="selectable break-words px-6 pb-4 text-destructive-foreground text-sm">{error}</p>}
          <AlertDialogFooter>
            <AlertDialogClose render={<Button variant="outline" disabled={busy === "remove"} />}>Cancel</AlertDialogClose>
            <Button variant="destructive" loading={busy === "remove"} onClick={() => void remove()}>Remove provider</Button>
          </AlertDialogFooter>
        </AlertDialogPopup>
      </AlertDialog>
    </li>
  );
}

/** Settings > AI providers > provider manifests: import, review, trust, edit and remove. */
export function ProviderManifests() {
  const manifests = useLLM((state) => state.manifests);
  const ready = useLLM((state) => state.manifestsReady);
  const error = useLLM((state) => state.manifestsError);
  const [adding, setAdding] = useState(false);

  return (
    <section aria-labelledby="provider-manifests-title" className="flex min-w-0 flex-col gap-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 id="provider-manifests-title" className="text-sm font-medium">Custom providers</h2>
        <Button size="sm" variant="outline" onClick={() => setAdding(true)}><Plus data-icon="inline-start" />Add provider</Button>
      </div>
      <p className="rounded-lg border bg-muted/40 p-3 text-muted-foreground">
        Provider manifests contain no secrets. Each one is pinned to the host you review, and your API key is only sent to that host. Plain http is allowed only for localhost, 127.0.0.1 and ::1. A new manifest is imported disabled; editing it requires a new review.
      </p>
      {error && <Failure error={error} retry={() => void useLLM.getState().loadManifests()} />}
      {ready && manifests.length === 0 && <p role="status" className="rounded-lg border bg-card p-3 text-muted-foreground">No custom providers yet.</p>}
      {manifests.length > 0 && <ul aria-label="Custom providers" className="divide-y rounded-lg border bg-card">{manifests.map((record) => <ManifestRow key={record.manifest.id} record={record} />)}</ul>}
      {adding && <ImportProviderDialog onClose={() => setAdding(false)} />}
    </section>
  );
}
