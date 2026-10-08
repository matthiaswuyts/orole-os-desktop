import { useCallback, useState } from "react";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Field, FieldLabel } from "@/components/ui/field";
import { NeedsDesktop } from "@/features/shared/States";
import { inDesktop } from "@/lib/backend";
import { useDaemon } from "@/stores/daemon";
import { api, errorText, type Config } from "./api";
import { Failure, NumberField } from "./fields";
import { useAutomationResource } from "./useAutomationResource";

export function AutomationSettings() {
  const load = useCallback(() => api.config(), []);
  const resource = useAutomationResource(load);
  const offline = useDaemon((s) => s.link === "offline");
  const [draft, setDraft] = useState<Config | null>(null);
  const [busy, setBusy] = useState(false);
  const [rotate, setRotate] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const config = draft ?? resource.data;
  if (!inDesktop) return <NeedsDesktop what="Automation settings" />;
  const patch = (value: Partial<Config>) => { if (config) setDraft({ ...config, ...value }); setMessage(null); };
  const save = async () => {
    if (!config) return;
    setBusy(true); setError(null); setMessage(null);
    try { setDraft(await api.setConfig(config)); setMessage("Automation settings saved."); await resource.refresh(); }
    catch (e) { setError(errorText(e)); }
    finally { setBusy(false); }
  };
  const generate = async () => {
    setBusy(true); setError(null); setMessage(null);
    try {
      await api.generateSecret();
      setDraft((previous) => previous ? { ...previous, hasSecret: true } : previous);
      setRotate(false); setMessage("Webhook secret saved securely.");
      await resource.refresh();
    } catch (e) { setError(errorText(e)); }
    finally { setBusy(false); }
  };
  return <section className="flex max-w-2xl flex-col gap-4">
    {offline && <Alert variant="warning"><AlertTitle>Daemon offline</AlertTitle><AlertDescription>Settings may be stale. Reconnect before making changes.</AlertDescription></Alert>}
    {(error || resource.error) && <Failure message={error || resource.error || ""} />}
    {resource.loading && <p role="status">Loading automation settings…</p>}
    {message && <p role="status" className="text-sm text-muted-foreground">{message}</p>}
    {!config && !resource.loading && <Button size="sm" variant="outline" className="self-start" onClick={() => void resource.refresh()}>Retry</Button>}
    {config && <form onSubmit={(e) => { e.preventDefault(); void save(); }}>
      <fieldset disabled={busy} className="flex min-w-0 flex-col gap-6">
        <fieldset disabled={offline} aria-labelledby="automation-run-limits" className="flex min-w-0 flex-col gap-3">
          <header>
            <h2 id="automation-run-limits" className="text-sm font-medium">Run limits</h2>
            <p className="text-xs text-muted-foreground">How many runs can happen at once.</p>
          </header>
          <div className="grid gap-4 sm:grid-cols-2">
            <NumberField label="Across all automations" value={config.globalConcurrency} min={1} max={32} onChange={(globalConcurrency) => patch({ globalConcurrency })} />
            <NumberField label="Per automation (default)" value={config.perAutomation} min={1} max={config.globalConcurrency} onChange={(perAutomation) => patch({ perAutomation })} />
          </div>
        </fieldset>
        <section aria-labelledby="automation-webhooks" className="flex flex-col gap-3 border-t pt-4">
          <Field className="flex-row items-center justify-between">
            <div>
              <h2 id="automation-webhooks"><FieldLabel htmlFor="automation-webhooks-enabled">Local webhooks</FieldLabel></h2>
              <p className="text-xs text-muted-foreground">Let apps on this PC trigger automations.</p>
            </div>
            <Switch id="automation-webhooks-enabled" checked={config.webhookEnabled} disabled={busy || offline} onCheckedChange={(webhookEnabled) => patch({ webhookEnabled })} />
          </Field>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <p className="text-xs text-muted-foreground">{config.hasSecret ? "Secret ready" : "Generate a secret before enabling webhooks."}</p>
            <Button type="button" size="sm" variant="outline" disabled={busy || offline} onClick={() => config.hasSecret ? setRotate(true) : void generate()}>{config.hasSecret ? "Replace secret…" : "Generate secret"}</Button>
          </div>
          {rotate && <Alert variant="warning">
            <AlertTitle>Replace the webhook secret?</AlertTitle>
            <AlertDescription>
              <p>Connected apps must reload the new secret before their next request.</p>
              <div className="flex flex-wrap gap-2">
                <Button type="button" size="sm" disabled={busy || offline} onClick={() => void generate()}>Replace secret</Button>
                <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => setRotate(false)}>Keep current secret</Button>
              </div>
            </AlertDescription>
          </Alert>}
          <details className="text-xs">
            <summary className="min-h-6 cursor-pointer rounded-sm font-medium outline-none focus-visible:ring-2 focus-visible:ring-ring">Webhook connection details</summary>
            <div className="mt-3 flex flex-col gap-3">
              <fieldset disabled={offline} className="max-w-48"><NumberField label="Webhook port" value={config.webhookPort} min={1024} max={65535} onChange={(webhookPort) => patch({ webhookPort })} /></fieldset>
              <p className="text-muted-foreground">Only authenticated requests from this PC are accepted, not network connections.</p>
              <p className="text-muted-foreground">The secret stays in Windows Credential Manager and never enters this interface. External tools must read the <code>automations/webhook-hmac</code> credential. Do not paste credentials into automation parameters.</p>
              <p className="selectable break-words text-muted-foreground">POST <code>http://127.0.0.1:{config.webhookPort}/automations/&#123;id&#125;</code> with <code>X-Orole-Timestamp</code> (Unix seconds) and <code>X-Orole-Signature: v1=&#123;hex&#125;</code>. The versioned HMAC-SHA256 envelope binds the method, canonical path, timestamp, <code>X-Orole-Chain</code> and exact body bytes. Sender format: <code>docs/architecture/automations.md</code>.</p>
            </div>
          </details>
        </section>
        <Button type="submit" size="sm" className="self-start" disabled={busy || offline}>{busy ? "Saving…" : "Save settings"}</Button>
      </fieldset>
    </form>}
  </section>;
}
