import { useCallback, useEffect, useState } from "react";
import { Copy, Play, Plus, RefreshCw, Trash2, Workflow } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { inDesktop } from "@/lib/backend";
import { useDaemon } from "@/stores/daemon";
import { NeedsDesktop } from "@/features/shared/States";
import { api, errorText, newDefinition, type Definition, type StepType } from "./api";
import { AutomationEditor } from "./AutomationEditor";
import { ApprovalPrompt, RunHistory, RunStatus } from "./RunHistory";
import { Failure } from "./fields";
import { useAutomationResource } from "./useAutomationResource";

export function AutomationsView() {
  const offline = useDaemon((s) => s.link === "offline");
  const load = useCallback(async () => { const [items, approvals] = await Promise.all([api.list(), api.approvals()]); return { items: items ?? [], approvals: approvals ?? [] }; }, []);
  const resource = useAutomationResource(load);
  const [types, setTypes] = useState<StepType[]>([]);
  const [templates, setTemplates] = useState<Definition[]>([]);
  const [catalogError, setCatalogError] = useState<string | null>(null);
  const [catalogLoaded, setCatalogLoaded] = useState(false);
  const [catalogRevision, setCatalogRevision] = useState(0);
  const [selected, setSelected] = useState<string | null>(null);
  const [draft, setDraft] = useState<{ key: number; definition: Definition } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);
  useEffect(() => {
    if (!inDesktop || offline) return;
    let active = true;
    setCatalogLoaded(false);
    Promise.all([api.stepTypes(), api.templates()]).then(([nextTypes, nextTemplates]) => { if (active) { setTypes(nextTypes ?? []); setTemplates(nextTemplates ?? []); setCatalogError(null); setCatalogLoaded(true); } }, (e) => { if (active) setCatalogError(errorText(e)); });
    return () => { active = false; };
  }, [offline, catalogRevision]);
  const edit = (definition: Definition) => { setDraft({ key: Date.now(), definition }); setError(null); setMessage(null); setDeleting(false); };
  const perform = async (action: () => Promise<void>) => {
    setBusy(true); setError(null); setMessage(null);
    try { await action(); await resource.refresh(); }
    catch (e) { setError(errorText(e)); }
    finally { setBusy(false); }
  };
  if (!inDesktop) return <NeedsDesktop what="Automations" />;
  const current = resource.data?.items.find((s) => s.definition.id === selected) ?? resource.data?.items[0];
  return <div className="flex h-full min-h-0 flex-col overflow-auto">
    <header className="flex min-h-10 flex-wrap items-center justify-end gap-2 border-b px-3 py-2"><Button size="xs" variant="outline" onClick={() => { void resource.refresh(); setCatalogRevision((n) => n + 1); }} disabled={busy}><RefreshCw data-icon="inline-start" />Refresh</Button><Button size="xs" onClick={() => edit(newDefinition())} disabled={!!draft || !catalogLoaded || offline}><Plus data-icon="inline-start" />New automation</Button></header>
    <div className="flex flex-col gap-3 p-3">
      {offline && <Alert variant="warning"><AlertTitle>Daemon offline</AlertTitle><AlertDescription>Results below may be stale. Reconnect the daemon to run or save automations.</AlertDescription></Alert>}
      {(resource.error || error) && <Failure message={error || resource.error || ""} />}
      {catalogError && <Failure message={`Could not load step types and presets: ${catalogError}. Use Refresh to try again.`} />}
      {message && <p role="status" className="text-sm text-muted-foreground">{message}</p>}
      {resource.data?.approvals.map((approval) => <ApprovalPrompt key={`${approval.runId}:${approval.stepId}`} approval={approval} offline={offline} onResolved={() => void resource.refresh()} />)}
    </div>
    <div className="flex min-h-0 flex-1 flex-col lg:flex-row">
      <aside aria-label="Saved automations" className="flex shrink-0 flex-col gap-3 border-b p-3 lg:w-72 lg:overflow-auto lg:border-r lg:border-b-0">
        {resource.loading && <p role="status">Loading automations…</p>}
        {!resource.loading && resource.data?.items.length === 0 && <Empty><EmptyHeader><EmptyMedia variant="icon"><Workflow /></EmptyMedia><EmptyTitle>No automations yet</EmptyTitle><EmptyDescription>Create a workflow or start from a preset. Nothing runs until you save and enable it or choose Run now.</EmptyDescription></EmptyHeader></Empty>}
        <ul className="flex flex-col gap-1">{resource.data?.items.map(({ definition, lastStatus, lastRunAt }) => <li key={definition.id} className="rounded-md border"><div className="flex items-center gap-2 p-2"><button type="button" className="min-w-0 flex-1 rounded-sm p-1 text-left outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60 aria-current:bg-accent" disabled={!!draft} aria-current={current?.definition.id === definition.id ? "true" : undefined} onClick={() => { setSelected(definition.id); setDeleting(false); }}><span className="block truncate font-medium">{definition.name}</span><span className="block text-xs text-muted-foreground">{definition.trigger.type}</span></button><Switch aria-label={`Enable ${definition.name}`} checked={definition.enabled} disabled={busy || offline || !!draft} onCheckedChange={(enabled) => void perform(async () => { const result = await api.save({ ...definition, enabled }); if (!result.saved) throw new Error((result.issues ?? []).map((i) => `${i.path}: ${i.message}`).join("; ") || "Definition was not saved."); })} /></div><div className="flex flex-wrap items-center gap-2 px-2 pb-2"><RunStatus status={lastStatus} />{lastRunAt > 0 && <time className="text-xs text-muted-foreground" dateTime={new Date(lastRunAt).toISOString()}>{new Date(lastRunAt).toLocaleString()}</time>}</div></li>)}</ul>
        <section className="flex flex-col gap-2"><h2 className="font-medium text-sm">Presets</h2>{offline && <p className="text-xs text-muted-foreground">Reconnect the daemon to use presets.</p>}{!offline && !catalogLoaded && !catalogError && <p role="status" className="text-xs text-muted-foreground">Loading presets…</p>}{catalogLoaded && !templates.length && <p className="text-xs text-muted-foreground">No presets provided by this daemon.</p>}{templates.map((template, index) => <Button key={`${template.id}:${index}`} size="sm" variant="outline" disabled={!!draft || offline} onClick={() => edit({ ...structuredClone(template), id: "", enabled: false })}>{template.name}</Button>)}{templates.length > 0 && <p className="text-xs text-muted-foreground">Presets open an unsaved draft. Fill in real project details before saving.</p>}</section>
      </aside>
      <main className="min-w-0 flex-1 p-4 lg:overflow-auto">
        {draft ? <AutomationEditor key={draft.key} initial={draft.definition} types={types} offline={offline} onSaved={(definition) => { setSelected(definition.id); setDraft(null); setMessage(`Saved ${definition.name}.`); void resource.refresh(); }} onClose={() => setDraft(null)} /> : current ? <div className="flex flex-col gap-5"><header className="flex flex-wrap items-start justify-between gap-3"><div className="min-w-0 flex-1"><h2 className="break-words text-base font-medium">{current.definition.name}</h2><p className="whitespace-pre-wrap break-words text-sm text-muted-foreground">{current.definition.description}</p></div><div className="flex flex-wrap gap-2"><Button size="sm" disabled={busy || offline} onClick={() => void perform(async () => { const run = await api.run(current.definition.id); setMessage(`Run ${run.id} ${run.status}.`); })}><Play data-icon="inline-start" />Run now</Button><Button size="sm" variant="outline" disabled={!catalogLoaded || busy} onClick={() => edit(current.definition)}>Edit</Button><Button size="sm" variant="outline" disabled={!catalogLoaded || busy} onClick={() => edit({ ...structuredClone(current.definition), id: "", name: `${current.definition.name} copy`, enabled: false })}><Copy data-icon="inline-start" />Duplicate</Button><Button size="sm" variant="destructive-outline" disabled={busy || offline} onClick={() => setDeleting(true)}><Trash2 data-icon="inline-start" />Delete</Button></div></header>
          {deleting && <Alert variant="warning"><AlertTitle>Delete {current.definition.name}?</AlertTitle><AlertDescription><p>This removes the saved definition. This action cannot be undone.</p><div className="flex gap-2"><Button size="sm" variant="destructive" disabled={busy || offline} onClick={() => void perform(async () => { await api.delete(current.definition.id); setDeleting(false); setSelected(null); })}>Delete automation</Button><Button size="sm" variant="outline" onClick={() => setDeleting(false)} disabled={busy}>Keep automation</Button></div></AlertDescription></Alert>}
          <dl className="grid gap-2 text-sm sm:grid-cols-3"><div><dt className="text-xs text-muted-foreground">Trigger</dt><dd>{current.definition.trigger.type}{current.definition.trigger.type === "schedule" && <code className="ml-2">{current.definition.trigger.schedule}</code>}</dd></div><div><dt className="text-xs text-muted-foreground">Steps</dt><dd>{current.definition.steps?.length ?? 0}</dd></div><div><dt className="text-xs text-muted-foreground">Automatic triggers</dt><dd>{current.definition.enabled ? "Enabled" : "Disabled"}</dd></div></dl>
          <RunHistory key={current.definition.id} automationId={current.definition.id} offline={offline} />
        </div> : !resource.loading && <p className="text-sm text-muted-foreground">Choose New automation or a preset to build a workflow. Run results and approval requests appear here.</p>}
      </main>
    </div>
  </div>;
}
