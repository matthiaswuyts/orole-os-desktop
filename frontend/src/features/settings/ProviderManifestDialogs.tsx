import { useRef, useState } from "react";
import type { ProviderManifest } from "@/mygo";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Dialog, DialogDescription, DialogFooter, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Textarea } from "@/components/ui/textarea";
import { useLLM } from "@/stores/llm";

/** The daemon rejects larger manifests; checking here only avoids reading an absurd file. */
const MAX_BYTES = 32 * 1024;

/** Static help text only; it is never saved or sent anywhere. */
const EXAMPLE_MANIFEST = `{
  "schemaVersion": 1,
  "id": "ollama",
  "name": "Ollama (this computer)",
  "baseUrl": "http://localhost:11434/v1",
  "auth": { "type": "none" },
  "models": [
    { "id": "llama3.2", "vision": false, "pdf": false, "tools": true, "reasoning": false }
  ]
}`;

type Json = Record<string, unknown>;
const isObject = (value: unknown): value is Json => typeof value === "object" && value !== null && !Array.isArray(value);
const text = (value: unknown) => typeof value === "string" ? value : "";

/** Local, display-only parse. The daemon validates for real and its message is shown as-is. */
function parseManifestJson(source: string): { manifest: ProviderManifest } | { error: string } {
  let value: unknown;
  try { value = JSON.parse(source); } catch (cause) { return { error: `This is not valid JSON: ${cause instanceof Error ? cause.message : String(cause)}` }; }
  if (!isObject(value)) return { error: "A provider manifest is a single JSON object." };
  return { manifest: value as unknown as ProviderManifest };
}

/** Everything the user must check before trusting: host first, then auth, endpoints, headers and models. */
export function ManifestSummary({ manifest }: { manifest: ProviderManifest }) {
  const raw = manifest as unknown as Json;
  const baseUrl = text(raw.baseUrl);
  let host = "";
  try { host = new URL(baseUrl).host; } catch { /* shown as unknown below */ }
  const auth = isObject(raw.auth) ? raw.auth : {};
  const authType = text(auth.type);
  const headers = isObject(raw.headers) ? Object.entries(raw.headers) : [];
  const models = Array.isArray(raw.models) ? raw.models.filter(isObject) : [];
  const authText = authType === "bearer" ? "Your API key is sent as a Bearer token to this host only."
    : authType === "header" ? `Your API key is sent in the "${text(auth.header)}" header to this host only.`
    : authType === "none" ? "No API key is stored or sent."
    : "Unknown (the daemon will reject this).";
  return (
    <div className="flex min-w-0 flex-col gap-3">
      <div className="flex min-w-0 flex-col gap-1 rounded-lg border bg-muted/40 p-3">
        <span className="text-xs text-muted-foreground">Pinned host: requests go to this host only</span>
        <span className="selectable break-all font-mono text-base font-semibold">{host || "unknown host"}</span>
        <span className="selectable break-all font-mono text-xs text-muted-foreground">{baseUrl}</span>
      </div>
      <dl className="grid min-w-0 grid-cols-[auto_1fr] gap-x-3 gap-y-1.5 text-sm">
        <dt className="text-muted-foreground">Provider</dt>
        <dd className="selectable min-w-0 break-words">{text(raw.name)} <span className="font-mono text-xs text-muted-foreground">({text(raw.id)})</span></dd>
        {text(raw.description) && <><dt className="text-muted-foreground">Description</dt><dd className="selectable min-w-0 whitespace-pre-wrap break-words">{text(raw.description)}</dd></>}
        <dt className="text-muted-foreground">Auth</dt>
        <dd className="min-w-0 break-words"><span className="font-mono">{authType || "?"}</span>: {authText}</dd>
        <dt className="text-muted-foreground">Endpoints</dt>
        <dd className="selectable min-w-0 break-all font-mono text-xs">models {text(raw.modelsPath) || "/models (default)"} · chat {text(raw.chatPath) || "/chat/completions (default)"}</dd>
        <dt className="text-muted-foreground">Static headers</dt>
        <dd className="min-w-0">
          {headers.length === 0 ? "None" : (
            <ul className="flex flex-col gap-0.5">{headers.map(([name, value]) => <li key={name} className="selectable break-all font-mono text-xs">{name}: {String(value)}</li>)}</ul>
          )}
        </dd>
        <dt className="text-muted-foreground">Models</dt>
        <dd className="min-w-0">
          {models.length === 0 ? "Listed by the provider's models endpoint" : (
            <ul className="flex flex-col gap-0.5">
              {models.map((model, index) => {
                const flags = (["vision", "pdf", "tools", "reasoning"] as const).filter((flag) => model[flag] === true);
                return <li key={index} className="selectable break-all text-xs"><span className="font-mono">{text(model.id)}</span>{flags.length > 0 && <span className="text-muted-foreground"> · {flags.join(", ")}</span>}</li>;
              })}
            </ul>
          )}
        </dd>
      </dl>
    </div>
  );
}

/** Pasting or choosing a manifest, reviewing it, and saving it disabled. */
export function ImportProviderDialog({ onClose }: { onClose: () => void }) {
  const [source, setSource] = useState("");
  const [review, setReview] = useState<ProviderManifest | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const fileRequest = useRef(0);

  function edit(value: string) {
    ++fileRequest.current;
    setSource(value); setReview(null); setError(null);
  }

  async function readFile(file: File) {
    const request = ++fileRequest.current;
    setReview(null); setError(null);
    if (file.size > MAX_BYTES) { setSource(""); setError("The manifest is larger than 32 KiB. Choose a smaller JSON file."); return; }
    try {
      const content = await file.text();
      if (request === fileRequest.current) setSource(content);
    } catch (cause) {
      if (request === fileRequest.current) setError(`Could not read the file: ${cause instanceof Error ? cause.message : String(cause)}`);
    }
  }

  function inspect() {
    const parsed = parseManifestJson(source);
    if ("error" in parsed) { setReview(null); setError(parsed.error); return; }
    setError(null);
    setReview(parsed.manifest);
  }

  async function save() {
    if (!review || saving) return;
    setSaving(true); setError(null);
    const result = await useLLM.getState().saveManifest(review, "");
    setSaving(false);
    if (result.ok) onClose();
    else setError(result.error);
  }

  return (
    <Dialog open onOpenChange={(open) => { if (!open && !saving) onClose(); }}>
      <DialogPopup>
        <DialogHeader>
          <DialogTitle>Add provider</DialogTitle>
          <DialogDescription>
            Describe an OpenAI-compatible provider with a manifest. It is imported disabled; you review and trust it afterwards. Manifests contain no secrets.
          </DialogDescription>
        </DialogHeader>
        <DialogPanel className="flex flex-col gap-4">
          {!review && (
            <>
              <Field>
                <FieldLabel htmlFor="provider-manifest-json">Manifest JSON</FieldLabel>
                <Textarea id="provider-manifest-json" value={source} onChange={(event) => edit(event.target.value)} rows={9} spellCheck={false} className="selectable w-full font-mono" aria-invalid={!!error} />
                <FieldDescription>Schema version 1, up to 32 KiB. Paste JSON or choose a file.</FieldDescription>
              </Field>
              <Field>
                <FieldLabel htmlFor="provider-manifest-file">Choose a .json file</FieldLabel>
                <input id="provider-manifest-file" type="file" accept=".json,application/json" className="block min-h-8 w-full rounded-md text-sm outline-none file:mr-3 file:rounded-md file:border file:border-border file:bg-background file:px-3 file:py-1.5 file:text-foreground focus-visible:ring-2 focus-visible:ring-ring" onChange={(event) => { const file = event.target.files?.[0]; event.target.value = ""; if (file) void readFile(file); }} />
              </Field>
              <details>
                <summary className="min-h-6 cursor-pointer rounded-sm text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring">Example: Ollama on this computer</summary>
                <pre aria-label="Example manifest" className="selectable mt-2 overflow-x-auto rounded-lg border bg-muted/40 p-3 font-mono text-xs">{EXAMPLE_MANIFEST}</pre>
              </details>
              <p className="text-xs text-muted-foreground">
                Manifests are pinned to the host they name: your key is only ever sent there. http is accepted only for localhost, 127.0.0.1 and ::1; every other host must use https. Static headers are never for credentials.
              </p>
            </>
          )}
          {review && (
            <>
              <p className="text-xs text-muted-foreground">This is a local preview. The daemon validates the manifest when you import it. Nothing is enabled until you trust it.</p>
              <ManifestSummary manifest={review} />
            </>
          )}
          {error && (
            <Alert variant="error">
              <AlertTitle>{review ? "The daemon rejected this manifest" : "Cannot review this manifest"}</AlertTitle>
              <AlertDescription className="selectable break-words">{error}</AlertDescription>
            </Alert>
          )}
        </DialogPanel>
        <DialogFooter>
          <Button type="button" variant="outline" disabled={saving} onClick={onClose}>Cancel</Button>
          {review ? (
            <>
              <Button type="button" variant="ghost" disabled={saving} onClick={() => { setReview(null); setError(null); }}>Back to editing</Button>
              <Button type="button" loading={saving} onClick={() => void save()}>Import disabled provider</Button>
            </>
          ) : (
            <Button type="button" disabled={!source.trim()} onClick={inspect}>Review</Button>
          )}
        </DialogFooter>
      </DialogPopup>
    </Dialog>
  );
}

/** Replaces a stored manifest; the daemon clears trust when the canonical manifest changes. */
export function EditManifestDialog({ id, digest, manifest, onClose }: { id: string; digest: string; manifest: ProviderManifest; onClose: () => void }) {
  const [source, setSource] = useState(() => JSON.stringify(manifest, null, 2));
  const [error, setError] = useState<string | null>(null);
  const [conflict, setConflict] = useState(false);
  const [saving, setSaving] = useState(false);
  // The record can change under an open editor (another window, an event); saving over it would be a blind overwrite.
  const live = useLLM((state) => state.manifests.find((r) => r.manifest.id === id));
  const stale = !live || live.digest !== digest;

  async function save() {
    const parsed = parseManifestJson(source);
    if ("error" in parsed) { setError(parsed.error); return; }
    if (parsed.manifest.id !== id) { setError(`The id must stay "${id}". To add a different provider, use Add provider.`); return; }
    setSaving(true); setError(null); setConflict(false);
    const result = await useLLM.getState().saveManifest(parsed.manifest, digest);
    setSaving(false);
    if (result.ok) onClose();
    else { setError(result.error); setConflict(result.code === "conflict"); }
  }

  return (
    <Dialog open onOpenChange={(open) => { if (!open && !saving) onClose(); }}>
      <DialogPopup>
        <DialogHeader>
          <DialogTitle>Edit {manifest.name} JSON</DialogTitle>
          <DialogDescription>
            Any change to the manifest disables the provider and requires a new review. If the host changes, the stored API key is deleted so it is never sent to a host you did not review.
          </DialogDescription>
        </DialogHeader>
        <DialogPanel className="flex flex-col gap-4">
          <Field>
            <FieldLabel htmlFor="provider-manifest-edit">Manifest JSON</FieldLabel>
            <Textarea id="provider-manifest-edit" value={source} onChange={(event) => { setSource(event.target.value); setError(null); setConflict(false); }} rows={14} spellCheck={false} className="selectable w-full font-mono" aria-invalid={!!error} />
            <FieldDescription className="selectable break-all">Replacing digest {digest}</FieldDescription>
          </Field>
          {stale && (
            <Alert variant="warning">
              <AlertTitle>This manifest changed</AlertTitle>
              <AlertDescription>It was changed or removed since you opened this editor. Close it and open the current version.</AlertDescription>
            </Alert>
          )}
          {error && (
            <Alert variant="error">
              <AlertTitle>Could not save</AlertTitle>
              <AlertDescription className="selectable break-words">
                {error}
                {conflict && " The saved manifest changed; close this editor and open the current version."}
              </AlertDescription>
            </Alert>
          )}
        </DialogPanel>
        <DialogFooter>
          <Button type="button" variant="outline" disabled={saving} onClick={onClose}>Cancel</Button>
          <Button type="button" loading={saving} disabled={stale || !source.trim()} onClick={() => void save()}>Save changes</Button>
        </DialogFooter>
      </DialogPopup>
    </Dialog>
  );
}
