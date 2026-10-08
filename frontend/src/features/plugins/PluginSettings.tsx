import { useRef, useState } from "react";
import { Plugins, type PluginManifest, type PluginRecord } from "@/mygo";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Fieldset } from "@/components/ui/fieldset";
import { Textarea } from "@/components/ui/textarea";
import { Failure, Loading, NoResults, errorText } from "@/features/mcp/shared";
import { NeedsDesktop } from "@/features/shared/States";
import { inDesktop } from "@/lib/backend";
import { isPluginActive, usePlugins } from "@/stores/plugins";
import { MAX_MANIFEST_BYTES, parseManifest } from "./manifest";
import { PluginJson } from "./PluginJson";

interface Review { manifest: PluginManifest; expectedDigest: string }

export function PluginSettings() {
  const { records, ready, loading, error: loadError, load } = usePlugins();
  const [text, setText] = useState("");
  const [review, setReview] = useState<Review | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [reading, setReading] = useState(false);
  const textarea = useRef<HTMLTextAreaElement>(null);
  const fileSequence = useRef(0);
  if (!inDesktop) return <NeedsDesktop what="Plugins" />;

  function updateText(value: string) {
    ++fileSequence.current;
    setText(value); setReading(false); setReview(null); setError(null); setNotice(null);
  }
  async function readFile(file: File) {
    const request = ++fileSequence.current;
    setText(""); setReading(false); setError(null); setReview(null); setNotice(null);
    if (file.size > MAX_MANIFEST_BYTES) { setError("The manifest exceeds 64 KiB. Choose a smaller JSON file."); return; }
    setReading(true);
    try {
      const value = await file.text();
      if (request === fileSequence.current) { setText(value); textarea.current?.focus(); }
    } catch (cause) { if (request === fileSequence.current) setError(`Could not read the file: ${errorText(cause)}`); }
    finally { if (request === fileSequence.current) setReading(false); }
  }
  function inspect() {
    setError(null); setNotice(null);
    try {
      const manifest = parseManifest(text);
      const current = records.find((record) => record.manifest.id === manifest.id);
      setReview({ manifest, expectedDigest: current?.digest ?? "" });
    } catch (cause) { setReview(null); setError(errorText(cause)); textarea.current?.focus(); }
  }
  async function save() {
    if (!review || busy) return;
    const inputRevision = fileSequence.current;
    setBusy(true); setError(null); setNotice(null);
    try {
      const saved = await Plugins.save(review);
      setNotice(`${saved.manifest.name} was saved. Inspect its canonical manifest below before choosing Trust and enable. Changed manifests always need trust again.`);
      if (inputRevision === fileSequence.current) { setReview(null); setText(""); }
      await load();
    } catch (cause) { setError(`${errorText(cause)} Refresh the plugin list, then review the JSON again if its saved version changed.`); void load(); }
    finally { setBusy(false); }
  }

  return <div className="flex min-w-0 flex-col gap-5">
    <p className="max-w-prose text-muted-foreground">Import declarative MCP plugins to add commands and dashboard widgets. This format does not load JavaScript, execute HTML, or receive credentials. It is not an arbitrary-code plugin system.</p>
    <Alert><AlertTitle>Inspect capabilities before trusting</AlertTitle><AlertDescription>Manifests, descriptions and tool output are untrusted data. Action arguments are stored as plaintext: never include secrets. Trust only enables the declared actions; MCP tool permissions and daemon policy still apply.</AlertDescription></Alert>
    <section aria-labelledby="plugin-import-title" className="flex min-w-0 flex-col gap-3">
      <h2 id="plugin-import-title" className="font-medium">Import or update a manifest</h2>
      <Fieldset disabled={busy} className="flex min-w-0 flex-col gap-3">
        <Field data-invalid={!!error}><FieldLabel htmlFor="plugin-manifest-json">Manifest JSON</FieldLabel><Textarea ref={textarea} id="plugin-manifest-json" disabled={busy} value={text} onChange={(event) => updateText(event.target.value)} aria-invalid={!!error} aria-describedby="plugin-manifest-help" rows={8} className="selectable w-full font-mono" spellCheck={false} /><FieldDescription id="plugin-manifest-help">Schema version 1, up to 64 KiB. Paste JSON or choose a file. Nothing is enabled on import.</FieldDescription></Field>
        <Field><FieldLabel htmlFor="plugin-manifest-file">Import from a JSON file</FieldLabel><input id="plugin-manifest-file" type="file" disabled={busy} accept=".json,application/json" className="block min-h-8 w-full rounded-md text-sm outline-none file:mr-3 file:rounded-md file:border file:border-border file:bg-background file:px-3 file:py-1.5 file:text-foreground focus-visible:ring-2 focus-visible:ring-ring" onChange={(event) => { const file = event.target.files?.[0]; event.target.value = ""; if (file) void readFile(file); }} /></Field>
        <div className="flex flex-wrap gap-2"><Button variant="outline" disabled={busy || reading || !text.trim() || !ready || loading} onClick={inspect}>Review JSON</Button>{text && <Button variant="ghost" disabled={busy} onClick={() => updateText("")}>Clear import</Button>}</div>
      </Fieldset>
      {reading && <Loading>Reading manifest file…</Loading>}
      <Failure error={error} />
      {review && <div className="flex min-w-0 flex-col gap-3 rounded-lg border bg-card p-3">
        <h3 className="break-words font-medium">{review.expectedDigest ? "Update" : "Import"} {review.manifest.name}</h3>
        <p className="text-xs text-muted-foreground">This is a local preview, not daemon validation. Saving a changed manifest revokes its previous trust and stops its active calls.</p>
        {review.expectedDigest && <p className="selectable break-all font-mono text-xs">Replacing digest: {review.expectedDigest}</p>}
        <ManifestInspection manifest={review.manifest} />
        <Button disabled={busy || !ready || loading} onClick={() => void save()}>{busy ? "Saving…" : review.expectedDigest ? "Save manifest update" : "Import disabled plugin"}</Button>
      </div>}
      {notice && <p role="status" className="text-sm text-muted-foreground">{notice}</p>}
    </section>
    <section aria-labelledby="installed-plugins-title" className="flex min-w-0 flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2"><h2 id="installed-plugins-title" className="font-medium">Imported plugins</h2><Button size="sm" variant="ghost" disabled={loading} onClick={() => void load()}>Refresh plugins</Button></div>
      <Failure error={loadError} retry={() => void load()} />
      {loading && <Loading>Loading saved manifests and trust…</Loading>}
      {ready && !records.length && <NoResults title="No plugins imported">Import a manifest above. Trusted commands appear in search; widgets are available through Home → Customize.</NoResults>}
      {records.map((record) => <PluginCard key={`${record.manifest.id}:${record.digest}`} record={record} onEdit={() => { updateText(JSON.stringify(record.manifest, null, 2)); textarea.current?.focus(); textarea.current?.scrollIntoView({ block: "center" }); }} />)}
    </section>
  </div>;
}

function ManifestInspection({ manifest }: { manifest: PluginManifest }) {
  return <div className="flex min-w-0 flex-col gap-3">
    <p className="selectable break-all font-mono text-xs">{manifest.id} · {manifest.version}</p>
    {manifest.description && <p className="whitespace-pre-wrap break-words text-sm text-muted-foreground">{manifest.description}</p>}
    <h4 className="text-xs font-medium">Requested server / tool capabilities</h4>
    {manifest.capabilities.length ? <ul className="flex flex-col gap-1 text-xs">{manifest.capabilities.map((capability, index) => <li key={index} className="selectable break-all font-mono">{capability.serverId} / {capability.tool}</li>)}</ul> : <p className="text-xs text-muted-foreground">No capabilities declared. The daemon rejects manifests without usable actions.</p>}
    <p className="text-xs text-muted-foreground">{manifest.actions.length} actions · {manifest.commands.length} commands · {manifest.widgets.length} widgets</p>
    <details><summary className="min-h-6 cursor-pointer rounded-sm text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring">Complete manifest, action arguments and contributions</summary><PluginJson value={manifest} label="Complete plugin manifest" /></details>
  </div>;
}

function PluginCard({ record, onEdit }: { record: PluginRecord; onEdit: () => void }) {
  const { ready, staleIds, load } = usePlugins();
  const [expanded, setExpanded] = useState(false);
  const [reviewed, setReviewed] = useState(false);
  const [removing, setRemoving] = useState(false);
  const [busy, setBusy] = useState<"trust" | "revoke" | "remove" | null>(null);
  const [error, setError] = useState<string | null>(null);
  const active = isPluginActive(record);
  const available = ready && !staleIds.includes(record.manifest.id);
  async function mutate(kind: "trust" | "revoke" | "remove") {
    if (busy || !available) return;
    setBusy(kind); setError(null);
    try {
      const ref = { id: record.manifest.id, digest: record.digest };
      if (kind === "remove") await Plugins.remove(ref);
      else await Plugins.trust({ ...ref, enabled: kind === "trust" });
      setRemoving(false); setReviewed(false);
      await load();
    } catch (cause) { setError(errorText(cause)); void load(); }
    finally { setBusy(null); }
  }
  return <section aria-label={record.manifest.name} className="flex min-w-0 flex-col gap-3 rounded-lg border bg-card p-3">
    <div className="flex flex-wrap items-center gap-2"><h3 className="min-w-0 break-words font-medium">{record.manifest.name}</h3><Badge variant={available && active ? "success" : "outline"}>{!available ? "Snapshot unavailable" : active ? "Trusted and enabled" : "Disabled / needs review"}</Badge><span className="text-xs text-muted-foreground">{record.manifest.version}</span></div>
    <p className="selectable break-all font-mono text-xs text-muted-foreground">{record.manifest.id}</p>
    <div className="flex flex-wrap gap-2">
      <Button size="sm" variant="outline" aria-expanded={expanded} onClick={() => setExpanded(!expanded)}>{expanded ? "Hide inspection" : "Inspect capabilities & manifest"}</Button>
      {active && <Button size="sm" variant="destructive-outline" disabled={!!busy || !available} onClick={() => void mutate("revoke")}>{busy === "revoke" ? "Revoking…" : "Revoke and disable"}</Button>}
      <Button size="sm" variant="ghost" disabled={!!busy || !available} onClick={onEdit}>Edit manifest JSON</Button>
      <Button size="sm" variant="destructive-outline" disabled={!!busy || !available} onClick={() => setRemoving(true)}>Remove plugin</Button>
    </div>
    <Failure error={error} />
    {expanded && <div className="flex min-w-0 flex-col gap-3">
      <ManifestInspection manifest={record.manifest} />
      <div><h4 className="text-xs font-medium">Canonical manifest SHA-256</h4><p className="selectable break-all font-mono text-xs">{record.digest}</p></div>
      <p className="text-xs text-muted-foreground">Saved {new Date(record.updatedAt).toLocaleString()}. Trust applies to this exact digest. Changing the manifest requires a new review.</p>
      {!active && <>
        <Field><FieldLabel htmlFor={`plugin-review-${record.manifest.id}`}><input id={`plugin-review-${record.manifest.id}`} type="checkbox" checked={reviewed} disabled={!!busy || !available} onChange={(event) => setReviewed(event.target.checked)} className="size-4 shrink-0 accent-primary" />I reviewed the complete manifest, fixed arguments and requested capabilities.</FieldLabel></Field>
        <Button disabled={!reviewed || !!busy || !available} onClick={() => void mutate("trust")}>{busy === "trust" ? "Awaiting daemon approval…" : "Trust and enable"}</Button>
      </>}
      {active && <p className="text-xs text-muted-foreground">Commands are available in search. Add widgets in Home → Customize; they run only when you choose Run action.</p>}
    </div>}
    {busy === "trust" && <p role="status" className="text-xs text-muted-foreground">Review the daemon’s separate approval request. Enabling a plugin does not approve future tool side effects.</p>}
    {removing && <Alert variant="warning"><AlertTitle>Remove {record.manifest.name}?</AlertTitle><AlertDescription>This deletes the saved manifest and revokes its actions. Existing dashboard widgets remain removable; remote data is not deleted.</AlertDescription><div className="flex flex-wrap gap-2"><Button size="sm" variant="outline" disabled={!!busy} onClick={() => setRemoving(false)}>Keep plugin</Button><Button size="sm" variant="destructive" disabled={!!busy || !available} onClick={() => void mutate("remove")}>{busy === "remove" ? "Removing…" : "Confirm removal"}</Button></div></Alert>}
  </section>;
}
