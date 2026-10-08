import { useState } from "react";
import { MCP, type Discovery, type ServerState } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Fieldset, FieldsetLegend } from "@/components/ui/fieldset";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { Dialog, DialogDescription, DialogFooter, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { errorText, Failure, JsonData, Loading, NoResults, useRemote } from "./shared";

export const QUICK_SERVERS = {
  mail: { name: "Orole Mail", url: "https://mail.orole.be/api/mcp" },
  drive: { name: "Orole Drive", url: "https://drive.orole.be/api/mcp" },
};

export function MCPSettings({ adapter }: { adapter?: "mail" | "drive" }) {
  const servers = useRemote(() => MCP.list(), "servers");
  const [adding, setAdding] = useState(false);
  const [preset, setPreset] = useState<"mail" | "drive" | undefined>(adapter);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const selected = (servers.data ?? []).filter((server) => !adapter || server.adapter === adapter);
  async function add(form: HTMLFormElement) {
    const data = new FormData(form);
    setBusy(true); setError(null);
    try {
      await MCP.add({ name: String(data.get("name")).trim(), url: String(data.get("url")).trim(), adapter: preset ?? "", authKind: "oauth", oauthClientId: String(data.get("clientId")).trim(), scopes: String(data.get("scopes")).trim() });
      setAdding(false); servers.reload();
    } catch (cause) { setError(errorText(cause)); }
    finally { setBusy(false); }
  }
  return <div className="flex min-w-0 flex-col gap-4">
    <p className="max-w-prose text-muted-foreground">Connect a Streamable HTTP MCP server. Tools and capabilities appear only after the server has been connected and discovered.</p>
    {!inDesktop && <NoResults title="Open the desktop app">MCP connections and credentials are managed by the local daemon, not this browser preview.</NoResults>}
    <div className="flex flex-wrap gap-2">
      {!adapter && <Button disabled={!inDesktop} onClick={() => { setPreset(undefined); setError(null); setAdding(true); }}>Add by URL</Button>}
      {adapter && <Button variant="outline" disabled={!inDesktop || (servers.data ?? []).some((server) => server.adapter === adapter)} onClick={() => { setPreset(adapter); setError(null); setAdding(true); }}>Add {adapter === "mail" ? "Mail" : "Drive"}</Button>}
      <Button variant="ghost" disabled={!inDesktop || servers.loading} onClick={servers.reload}>Refresh connections</Button>
    </div>
    <Failure error={servers.error} retry={servers.reload} />
    {servers.loading && <Loading>Loading connections…</Loading>}
    {inDesktop && servers.data && !selected.length && <NoResults title="No server connected">Add {adapter ?? "an MCP server"} to get started. Authorization happens in your browser.</NoResults>}
    {selected.map((server) => <ServerSettings key={server.id} server={server} changed={servers.reload} />)}
    <Dialog open={adding} onOpenChange={(open) => { if (!busy) setAdding(open); }}>
      <DialogPopup anchored showCloseButton={!busy}>
        <DialogHeader><DialogTitle>Add {preset ? QUICK_SERVERS[preset].name : "MCP server"}</DialogTitle><DialogDescription>Use an OAuth client registered for this server, or a supported client metadata URL.{preset && " Mail and Drive disable dynamic client registration."}</DialogDescription></DialogHeader>
        <form className="contents" onSubmit={(event) => { event.preventDefault(); void add(event.currentTarget); }}>
          <DialogPanel><Fieldset className="flex flex-col gap-4" disabled={busy}>
            <Field><FieldLabel htmlFor="mcp-name">Name</FieldLabel><Input id="mcp-name" name="name" required defaultValue={preset ? QUICK_SERVERS[preset].name : ""} /></Field>
            <Field><FieldLabel htmlFor="mcp-url">Server URL</FieldLabel><Input id="mcp-url" name="url" type="url" required defaultValue={preset ? QUICK_SERVERS[preset].url : ""} /></Field>
            <Field><FieldLabel htmlFor="mcp-client">Client ID or CIMD URL</FieldLabel><Input id="mcp-client" name="clientId" autoComplete="off" /><FieldDescription>A public OAuth client ID or HTTPS client ID metadata document URL. Blank requests dynamic registration only when supported.</FieldDescription></Field>
            <Field><FieldLabel htmlFor="mcp-scopes">Scopes (optional)</FieldLabel><Input id="mcp-scopes" name="scopes" /><FieldDescription>Space-separated. Blank uses the server’s requested scopes.</FieldDescription></Field>
            <Failure error={error} />
          </Fieldset></DialogPanel>
          <DialogFooter><Button type="button" variant="outline" disabled={busy} onClick={() => setAdding(false)}>Cancel</Button><Button type="submit" disabled={busy}>{busy ? "Adding…" : "Add server"}</Button></DialogFooter>
        </form>
      </DialogPopup>
    </Dialog>
  </div>;
}

function ServerSettings({ server, changed }: { server: ServerState; changed: () => void }) {
  const [expanded, setExpanded] = useState(false);
  const [remove, setRemove] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [authStarted, setAuthStarted] = useState(false);
  const [discovery, setDiscovery] = useState<Discovery | null>(null);
  const catalog = useRemote(() => MCP.catalog(server.id), server.id, expanded);
  const diagnostics = useRemote(() => MCP.diagnostics(server.id), server.id, expanded);
  async function run(action: () => Promise<unknown>) {
    setBusy(true); setError(null);
    try { await action(); changed(); catalog.reload(); diagnostics.reload(); }
    catch (cause) { setError(errorText(cause)); }
    finally { setBusy(false); }
  }
  return <section className="flex min-w-0 flex-col gap-3 rounded-lg border bg-card p-3" aria-label={server.name}>
    <div className="flex flex-wrap items-center gap-2"><h2 className="font-medium">{server.name}</h2><Badge variant={server.status === "connected" ? "success" : "outline"}>{server.status.replaceAll("_", " ")}</Badge><span className="ml-auto"><Switch aria-label={`Enable ${server.name}`} checked={server.enabled} disabled={busy} onCheckedChange={(enabled) => void run(() => MCP.enable(server.id, enabled))} /></span></div>
    <p className="selectable break-all text-xs text-muted-foreground">{server.url}</p>
    <Failure error={error ?? server.lastError ?? null} />
    <div className="flex flex-wrap gap-2">
      <Button size="sm" disabled={busy || !server.enabled} onClick={() => void run(async () => { await MCP.startAuth(server.id); setAuthStarted(true); })}>{server.authorized ? "Reauthorize" : "Authorize"}</Button>
      <Button size="sm" variant="outline" disabled={busy || !server.enabled} onClick={() => void run(() => MCP.connect(server.id))}>Connect</Button>
      <Button size="sm" variant="ghost" aria-expanded={expanded} onClick={() => setExpanded(!expanded)}>{expanded ? "Hide details" : `Tools & diagnostics (${server.toolCount})`}</Button>
      <Button size="sm" variant="destructive-outline" disabled={busy} onClick={() => setRemove(true)}>Remove</Button>
    </div>
    {authStarted && server.status !== "connected" && <div role="status" className="flex flex-wrap items-center gap-2 text-muted-foreground">Finish authorization in your browser.<Button size="sm" variant="outline" disabled={busy} onClick={() => void run(async () => { await MCP.cancelAuth(server.id); setAuthStarted(false); })}>Cancel authorization</Button></div>}
    <form key={`${server.id}:${server.oauthClientId}:${server.scopes}`} onSubmit={(event) => { event.preventDefault(); const data = new FormData(event.currentTarget); void run(() => MCP.update(server.id, { oauthClientId: String(data.get("clientId")).trim(), scopes: String(data.get("scopes")).trim() })); }}>
      <Fieldset disabled={busy} className="flex flex-col gap-3"><FieldsetLegend>OAuth client</FieldsetLegend>
        <Field><FieldLabel htmlFor={`client-${server.id}`}>Client ID or CIMD URL</FieldLabel><Input id={`client-${server.id}`} name="clientId" defaultValue={server.oauthClientId} /><FieldDescription>Public client configuration only. Access and refresh tokens never appear here.</FieldDescription></Field>
        <Field><FieldLabel htmlFor={`scopes-${server.id}`}>Scopes</FieldLabel><Input id={`scopes-${server.id}`} name="scopes" defaultValue={server.scopes} /></Field>
        <div className="flex flex-wrap gap-2"><Button type="submit" size="sm" variant="outline">Save client configuration</Button><Button size="sm" variant="ghost" onClick={() => void run(async () => setDiscovery(await MCP.discoverAuth(server.id)))}>Discover authorization</Button>{server.authorized && <Button size="sm" variant="ghost" onClick={() => void run(() => MCP.signOut(server.id))}>Sign out</Button>}</div>
      </Fieldset>
    </form>
    {discovery && <details open><summary className="cursor-pointer font-medium">Authorization discovery</summary><JsonData value={discovery} /></details>}
    {expanded && <div className="flex flex-col gap-3">
      <p className="text-xs text-muted-foreground">Server names, descriptions, schemas and annotations are untrusted data. Annotations are hints, not permission grants.</p>
      <Failure error={catalog.error} retry={catalog.reload} />
      {catalog.loading && <Loading>Loading discovered tools…</Loading>}
      <Button size="sm" variant="outline" disabled={busy || server.status !== "connected"} onClick={() => void run(() => MCP.refreshCatalog(server.id))}>Refresh discovered capabilities</Button>
      {catalog.data?.tools?.map((tool) => <details key={tool.name} className="rounded-md border p-2">
        <summary className="cursor-pointer break-words font-medium">{tool.title || tool.name} <Badge variant="outline">{tool.effect || "unclassified"}</Badge></summary>
        <div className="mt-3 flex flex-col gap-2"><code className="break-all text-xs">{tool.name}</code><p className="whitespace-pre-wrap break-words text-muted-foreground">{tool.description}</p>
          <Field><FieldLabel htmlFor={`permission-${server.id}-${tool.name}`}>Permission</FieldLabel><select id={`permission-${server.id}-${tool.name}`} className="rounded-md border bg-background p-2" disabled={busy} value={tool.permission || "ask"} onChange={(event) => void run(() => MCP.setToolPermission(server.id, tool.name, event.target.value))}><option value="ask">Ask</option><option value="allow">Allow (subject to policy)</option><option value="deny">Deny</option></select></Field>
          <h3 className="text-xs font-medium">Server annotations</h3><JsonData value={{ readOnlyHint: tool.readOnlyHint, destructiveHint: tool.destructiveHint, idempotentHint: tool.idempotentHint, openWorldHint: tool.openWorldHint }} />
          <h3 className="text-xs font-medium">Input schema</h3><JsonData value={tool.inputSchema} />{tool.outputSchema != null && <><h3 className="text-xs font-medium">Output schema</h3><JsonData value={tool.outputSchema} /></>}
        </div>
      </details>)}
      {catalog.data && !catalog.data.tools?.length && <p className="text-muted-foreground">No tools discovered.</p>}
      {catalog.data && <details><summary className="cursor-pointer font-medium">Resources and prompts</summary><JsonData value={{ resources: catalog.data.resources, prompts: catalog.data.prompts }} /></details>}
      <Failure error={diagnostics.error} retry={diagnostics.reload} />
      <details><summary className="cursor-pointer font-medium">Connection diagnostics</summary>{diagnostics.loading ? <Loading>Loading diagnostics…</Loading> : <JsonData value={diagnostics.data} />}</details>
    </div>}
    <Dialog open={remove} onOpenChange={(open) => { if (!busy) setRemove(open); }}><DialogPopup><DialogHeader><DialogTitle>Remove {server.name}?</DialogTitle><DialogDescription>This removes the connection and its stored credentials. It does not delete remote mail or files.</DialogDescription></DialogHeader><DialogPanel><Failure error={error} /></DialogPanel><DialogFooter><Button variant="outline" disabled={busy} onClick={() => setRemove(false)}>Keep server</Button><Button variant="destructive" disabled={busy} onClick={() => void run(() => MCP.remove(server.id))}>Remove server</Button></DialogFooter></DialogPopup></Dialog>
  </section>;
}
