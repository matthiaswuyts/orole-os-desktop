import { useEffect, useState } from "react";
import type { Account, ProviderStatus } from "@/mygo";
import { AlertDialog, AlertDialogClose, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogPopup, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ProviderManifests } from "@/features/settings/ProviderManifests";
import { Failure, Loading } from "@/features/mcp/shared";
import { ConnectProviderDialog } from "@/features/settings/ConnectProviderDialog";
import { ModelDefaults } from "@/features/settings/ModelDefaults";
import { NeedsDesktop } from "@/features/shared/States";
import { inDesktop } from "@/lib/backend";
import { useLLM, type Busy } from "@/stores/llm";

const usd = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD" });

function accountText(account: Account | undefined): string {
  if (!account) return "";
  const parts: string[] = [];
  if (account.label) parts.push(account.label);
  if (account.remaining !== undefined) {
    parts.push(`${usd.format(account.remaining)} credits left${account.limit ? ` of ${usd.format(account.limit)}` : ""}`);
  }
  return parts.join(" · ");
}

function StatusBadge({ provider, busy }: { provider: ProviderStatus; busy: Busy | undefined }) {
  if (busy === "test") return <Badge variant="info">Checking</Badge>;
  if (!provider.connected) return <Badge variant="outline">Not connected</Badge>;
  if (provider.valid === false) return <Badge variant="error">Invalid key</Badge>;
  return <Badge variant="success">Connected{provider.keyHint ? ` • ****${provider.keyHint}` : ""}</Badge>;
}

function ProviderRow({ provider, onConnect }: { provider: ProviderStatus; onConnect: () => void }) {
  const busy = useLLM((state) => state.busy[provider.id]);
  const [error, setError] = useState<string | null>(null);
  const [confirming, setConfirming] = useState(false);
  const authNone = useLLM((state) => provider.kind === "manifest" && state.manifests.find((r) => r.manifest.id === provider.id)?.manifest.auth.type === "none");
  const account = accountText(provider.account);

  async function test() {
    setError(null);
    const result = await useLLM.getState().test(provider.id);
    if (!result.ok) setError(result.error);
  }

  async function disconnect() {
    setError(null);
    const result = await useLLM.getState().disconnect(provider.id);
    if (result.ok) setConfirming(false);
    else setError(result.error);
  }

  const connect = <Button size="sm" disabled={!!busy} onClick={onConnect}>{provider.connected ? "Update key" : "Connect"}</Button>;
  return (
    <li aria-label={provider.name} className="flex min-w-0 flex-col gap-1.5 p-3">
      <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2">
        <span aria-hidden className="grid size-7 shrink-0 place-items-center rounded-md bg-muted text-sm font-semibold uppercase text-muted-foreground">{provider.name.charAt(0)}</span>
        <div className="flex min-w-0 flex-1 flex-col">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <h3 className="font-medium">{provider.name}</h3>
            <StatusBadge provider={provider} busy={busy} />
            {provider.kind === "manifest" && <Badge variant="secondary">Manifest</Badge>}
          </div>
          {account && <p className="truncate text-xs text-muted-foreground">{account}</p>}
        </div>
        <div className="flex shrink-0 items-center gap-1.5">
          {authNone ? (
            // No key exists for auth "none": the daemon connects it automatically, so there is nothing to connect or disconnect.
            <Button size="sm" variant="outline" loading={busy === "test"} disabled={!!busy} onClick={() => void test()}>Test</Button>
          ) : provider.connected ? (
            <>
              {provider.valid === false && connect}
              <Button size="sm" variant="outline" loading={busy === "test"} disabled={!!busy} onClick={() => void test()}>Test</Button>
              <Button size="sm" variant="destructive-outline" disabled={!!busy} onClick={() => setConfirming(true)}>Disconnect</Button>
            </>
          ) : connect}
        </div>
      </div>
      {provider.connected && provider.valid === false && provider.error && (
        <p role="alert" className="selectable break-words text-xs text-destructive-foreground">{provider.error}</p>
      )}
      {error && !confirming && <p role="alert" className="selectable break-words text-xs text-destructive-foreground">{error}</p>}
      <AlertDialog open={confirming} onOpenChange={(open) => { if (!open && busy !== "disconnect") setConfirming(false); }}>
        <AlertDialogPopup>
          <AlertDialogHeader>
            <AlertDialogTitle>Disconnect {provider.name}?</AlertDialogTitle>
            <AlertDialogDescription render={<div />}>
              This deletes the stored key from Windows Credential Manager. Bots that use {provider.name} models stop answering until you connect again. The key itself stays valid on {provider.name}'s site; revoke it there if you want it gone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          {error && <p role="alert" className="px-6 pb-4 text-destructive-foreground text-sm">{error}</p>}
          <AlertDialogFooter>
            <AlertDialogClose render={<Button variant="outline" disabled={busy === "disconnect"} />}>Cancel</AlertDialogClose>
            <Button variant="destructive" loading={busy === "disconnect"} onClick={() => void disconnect()}>Disconnect</Button>
          </AlertDialogFooter>
        </AlertDialogPopup>
      </AlertDialog>
    </li>
  );
}

/** Settings > AI providers: connect providers, check them, and pick default models. */
export function AIProviders() {
  const providers = useLLM((state) => state.providers);
  const ready = useLLM((state) => state.ready);
  const error = useLLM((state) => state.error);
  const [connecting, setConnecting] = useState<string | null>(null);
  useEffect(() => useLLM.getState().startSync(), []);
  if (!inDesktop) return <NeedsDesktop what="AI providers" />;
  const target = providers.find((p) => p.id === connecting);

  return (
    <div className="flex min-w-0 flex-col gap-5">
      <p className="rounded-lg border bg-muted/40 p-3 text-muted-foreground">
        API keys are stored in Windows Credential Manager, never in Orole's database or logs. Requests go directly from this computer to the provider; there is no Orole server in between.
      </p>
      {!ready && (error ? <Failure error={error} retry={() => void useLLM.getState().load()} /> : <Loading>Loading providers…</Loading>)}
      {ready && (
        <>
          <section aria-labelledby="ai-providers-title" className="flex min-w-0 flex-col gap-2">
            <h2 id="ai-providers-title" className="text-sm font-medium">Providers</h2>
            {providers.length === 0
              ? <p role="status" className="rounded-lg border bg-card p-3 text-muted-foreground">The daemon reports no providers.</p>
              : <ul className="divide-y rounded-lg border bg-card">{providers.map((p) => <ProviderRow key={p.id} provider={p} onConnect={() => setConnecting(p.id)} />)}</ul>}
          </section>
          <ProviderManifests />
          <ModelDefaults />
        </>
      )}
      {target && <ConnectProviderDialog key={target.id} provider={target} onClose={() => setConnecting(null)} />}
    </div>
  );
}
