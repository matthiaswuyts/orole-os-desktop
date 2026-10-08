import { useCallback, useState } from "react";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { NeedsDesktop } from "@/features/shared/States";
import { inDesktop } from "@/lib/backend";
import { useDaemon } from "@/stores/daemon";
import { api, errorText, type Config } from "./api";
import { Failure, FormField, NumberField } from "./fields";
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
      setRotate(false); setMessage("Webhook secret stored in Windows Credential Manager. No secret was sent to this interface.");
      await resource.refresh();
    } catch (e) { setError(errorText(e)); }
    finally { setBusy(false); }
  };
  return <section className="flex max-w-2xl flex-col gap-4"><header><h2 className="text-sm font-medium">Automations</h2><p className="text-muted-foreground">Daemon-owned execution limits and authenticated local webhooks.</p></header>
    {offline && <Alert variant="warning"><AlertTitle>Daemon offline</AlertTitle><AlertDescription>Settings may be stale. Reconnect before making changes.</AlertDescription></Alert>}
    {(error || resource.error) && <Failure message={error || resource.error || ""} />}
    {resource.loading && <p role="status">Loading automation settings…</p>}
    {message && <p role="status" className="text-sm text-muted-foreground">{message}</p>}
    {!config && !resource.loading && <Button size="sm" variant="outline" className="self-start" onClick={() => void resource.refresh()}>Retry</Button>}
    {config && <><form onSubmit={(e) => { e.preventDefault(); void save(); }}><fieldset disabled={busy || offline} className="flex min-w-0 flex-col gap-4"><div className="grid gap-4 sm:grid-cols-2"><NumberField label="Global concurrent runs" value={config.globalConcurrency} min={1} max={32} onChange={(globalConcurrency) => patch({ globalConcurrency })} /><NumberField label="Default concurrent runs per automation" value={config.perAutomation} min={1} max={config.globalConcurrency} onChange={(perAutomation) => patch({ perAutomation })} /></div><FormField label="Enable loopback webhooks" hint="The listener accepts only local authenticated requests, not network connections. Generate a secret below before enabling.">{(id) => <Switch id={id} checked={config.webhookEnabled} onCheckedChange={(webhookEnabled) => patch({ webhookEnabled })} />}</FormField><NumberField label="Webhook port" value={config.webhookPort} min={1024} max={65535} onChange={(webhookPort) => patch({ webhookPort })} /><Button type="submit" size="sm" className="self-start" disabled={busy || offline}>{busy ? "Saving…" : "Save settings"}</Button></fieldset></form>
      <section className="flex flex-col gap-2"><h3 className="text-sm font-medium">Webhook authentication</h3><p className="text-sm">{config.hasSecret ? "A webhook secret is stored." : "No webhook secret has been generated."}</p><p className="text-xs text-muted-foreground">The secret never enters the frontend. External tools must read the <code>automations/webhook-hmac</code> credential through Windows Credential Manager. Do not paste credentials into automation parameters.</p><p className="selectable break-all text-xs text-muted-foreground">POST http://127.0.0.1:{config.webhookPort}/automations/&#123;id&#125; with X-Orole-Timestamp (Unix seconds) and X-Orole-Signature: v1=&#123;hex&#125;. The versioned HMAC-SHA256 envelope binds the method, canonical path, timestamp, X-Orole-Chain and exact body bytes. Sender format: docs/architecture/automations.md.</p><Button size="sm" variant="outline" className="self-start" disabled={busy || offline} onClick={() => config.hasSecret ? setRotate(true) : void generate()}>{config.hasSecret ? "Rotate webhook secret…" : "Generate webhook secret"}</Button>
        {rotate && <Alert variant="warning"><AlertTitle>Replace the webhook secret?</AlertTitle><AlertDescription><p>Existing callers must reload the new credential before their next request.</p><div className="flex gap-2"><Button size="sm" disabled={busy || offline} onClick={() => void generate()}>Rotate secret</Button><Button size="sm" variant="outline" disabled={busy} onClick={() => setRotate(false)}>Keep current secret</Button></div></AlertDescription></Alert>}
      </section>
    </>}
  </section>;
}
