import { useState } from "react";
import { ChevronDown, Play, Plus, RefreshCw, Trash2, Workflow } from "lucide-react";
import { AlertDialog, AlertDialogClose, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogPopup, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Menu, MenuGroup, MenuGroupLabel, MenuItem, MenuPopup, MenuSeparator, MenuTrigger } from "@/components/ui/menu";
import { Switch } from "@/components/ui/switch";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { useChat } from "@/stores/chat";
import { api, errorText, newDefinition, type Approval, type Definition, type Summary, type Topic } from "./api";
import { Failure } from "./fields";
import { flatten, relativeTime, triggerSummary } from "./flow";
import { ApprovalPrompt, RunStatus } from "./RunsPanel";

export function FlowList({ items, approvals, loading, error, offline, topics, templates, catalog, onRefresh, onOpen, onCreate }: {
  items: Summary[] | null; approvals: Approval[]; loading: boolean; error: string | null; offline: boolean; topics: Topic[]; templates: Definition[];
  catalog: { ready: boolean; error: string | null; retry: () => void };
  onRefresh: () => void; onOpen: (summary: Summary, run?: string) => void; onCreate: (definition: Definition) => void;
}) {
  const [busy, setBusy] = useState<string | null>(null);
  const bots = useChat((s) => s.bots);
  const [failure, setFailure] = useState<string | null>(null);
  const [started, setStarted] = useState<{ summary: Summary; run: string } | null>(null);
  const [deleting, setDeleting] = useState<Summary | null>(null);
  const act = async (id: string, action: () => Promise<void>) => {
    setBusy(id); setFailure(null);
    try { await action(); onRefresh(); }
    catch (e) { setFailure(errorText(e)); }
    finally { setBusy(null); }
  };
  const toggle = (summary: Summary, enabled: boolean) => act(summary.definition.id, async () => {
    const result = await api.save({ ...summary.definition, enabled });
    if (!result.saved) throw new Error(`${summary.definition.name} was not saved: ${(result.issues ?? []).map((i) => i.message).join("; ") || "the daemon refused the change"}. Open it to fix the problems.`);
  });
  const run = (summary: Summary) => act(summary.definition.id, async () => {
    const r = await api.run(summary.definition.id);
    setStarted({ summary, run: r.id });
  });
  return <div className="flex h-full min-h-0 flex-col">
    <header className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1 border-b px-4 py-2.5">
      <div className="min-w-0 flex-1">
        <h1 className="text-sm font-medium">Automations</h1>
        <p className="text-xs text-muted-foreground">Flows start on a trigger and run their blocks in the daemon, also while this window is closed.</p>
      </div>
      <Button size="sm" variant="ghost" aria-label="Refresh automations" onClick={() => { onRefresh(); if (catalog.error) catalog.retry(); }}><RefreshCw /></Button>
      <Menu>
        <MenuTrigger render={<Button size="sm" disabled={offline || !catalog.ready} />}><Plus data-icon="inline-start" />New flow<ChevronDown data-icon="inline-end" /></MenuTrigger>
        <MenuPopup align="end" className="w-72">
          <MenuItem onClick={() => onCreate(newDefinition())}>Blank flow</MenuItem>
          {templates.length > 0 && <><MenuSeparator /><MenuGroup><MenuGroupLabel>Start from a template</MenuGroupLabel>
            {templates.map((t, i) => <MenuItem key={`${t.id}:${i}`} onClick={() => onCreate({ ...structuredClone(t), id: "", enabled: false })}>
              <span className="flex min-w-0 flex-col"><span className="truncate">{t.name}</span>{t.description && <span className="line-clamp-2 text-xs text-muted-foreground">{t.description}</span>}</span>
            </MenuItem>)}
          </MenuGroup></>}
        </MenuPopup>
      </Menu>
    </header>
    <div className="min-h-0 flex-1 overflow-auto">
      <div className="mx-auto flex max-w-3xl flex-col gap-3 p-4">
        {offline && <Alert variant="warning"><AlertTitle>Daemon offline</AlertTitle><AlertDescription>What you see may be out of date. Reconnect to run, save or create flows.</AlertDescription></Alert>}
        {(error || failure) && <Failure message={failure || error || ""} />}
        {catalog.error && <Failure message={`Could not load blocks and templates: ${catalog.error}. Use Refresh to try again.`} />}
        {started && <p role="status" className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">Started {started.summary.definition.name}.
          <Button size="xs" variant="outline" onClick={() => onOpen(started.summary, started.run)}>View run</Button></p>}
        {approvals.length > 0 && <section aria-label="Waiting for your approval" className="flex flex-col gap-2">
          <h2 className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Waiting for you</h2>
          {approvals.map((a) => <ApprovalPrompt key={`${a.runId}:${a.stepId}`} approval={a} offline={offline} onResolved={onRefresh} />)}
        </section>}
        {loading && !items && <p role="status" className="text-sm text-muted-foreground">Loading flows…</p>}
        {items && !items.length && <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon"><Workflow /></EmptyMedia>
            <EmptyTitle>No flows yet</EmptyTitle>
            <EmptyDescription>A flow starts on a trigger (a schedule, an app event, a GitHub change or a deployment) and runs blocks: ask a bot, start a code task, notify you. Nothing runs until you save and enable it, or choose Run now.</EmptyDescription>
          </EmptyHeader>
        </Empty>}
        {!!items?.length && <ul className="flex flex-col gap-2">{items.map((summary) => {
          const { definition: d, lastStatus, lastRunAt, nextRunAt, issues, watch, createdBy } = summary;
          const creator = createdBy?.startsWith("bot:") ? bots.find((b) => b.id === createdBy.slice(4))?.name ?? "a bot" : null;
          const blocks = flatten(d.steps ?? []).length;
          return <li key={d.id} className="rounded-lg border bg-card shadow-xs/5">
            <div className="flex items-center gap-3 px-3 py-2">
              <button type="button" onClick={() => onOpen(summary)} className="flex min-w-0 flex-1 flex-col gap-0.5 rounded-md text-left outline-none focus-visible:ring-2 focus-visible:ring-ring">
                <span className="flex min-w-0 items-center gap-2">
                  <span className="truncate text-sm font-medium">{d.name || "Untitled flow"}</span>
                  {issues?.length > 0 && <Badge size="sm" variant="error">{issues.length === 1 ? "1 problem" : `${issues.length} problems`}</Badge>}
                  {creator && <Badge size="sm" variant="secondary">By {creator}</Badge>}
                </span>
                <span className="truncate text-xs text-muted-foreground">{triggerSummary(d.trigger, topics)} · {blocks === 1 ? "1 block" : `${blocks} blocks`}</span>
                <span className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted-foreground tabular-nums">
                  {lastRunAt > 0 ? <span className="flex items-center gap-1.5"><RunStatus status={lastStatus} /><Tooltip><TooltipTrigger render={<time dateTime={new Date(lastRunAt).toISOString()} />}>{relativeTime(lastRunAt)}</TooltipTrigger><TooltipPopup>{new Date(lastRunAt).toLocaleString()}</TooltipPopup></Tooltip></span> : <span>Never run</span>}
                  {d.enabled && nextRunAt > 0 && <span>Next <Tooltip><TooltipTrigger render={<time dateTime={new Date(nextRunAt).toISOString()} />}>{relativeTime(nextRunAt)}</TooltipTrigger><TooltipPopup>{new Date(nextRunAt).toLocaleString()}</TooltipPopup></Tooltip></span>}
                  {d.enabled && watch?.polledAt ? <span>Checked {relativeTime(watch.polledAt)}</span> : null}
                </span>
                {watch?.error && <Tooltip><TooltipTrigger render={<span className="truncate text-xs text-destructive-foreground" />}>Watch failed: {watch.error}</TooltipTrigger><TooltipPopup className="max-w-sm break-words">{watch.error}</TooltipPopup></Tooltip>}
              </button>
              <Switch aria-label={`Enable ${d.name}`} checked={d.enabled} disabled={offline || busy === d.id} onCheckedChange={(enabled) => void toggle(summary, enabled)} />
              <Button size="xs" variant="outline" disabled={offline || busy === d.id || issues?.length > 0} onClick={() => void run(summary)}><Play data-icon="inline-start" />Run now</Button>
              <Button size="icon-xs" variant="ghost" aria-label={`Delete ${d.name}`} disabled={offline || busy === d.id} onClick={() => setDeleting(summary)}><Trash2 /></Button>
            </div>
          </li>;
        })}</ul>}
      </div>
    </div>
    <AlertDialog open={deleting !== null} onOpenChange={(open) => { if (!open && !busy) setDeleting(null); }}>
      <AlertDialogPopup>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete {deleting?.definition.name}?</AlertDialogTitle>
          <AlertDialogDescription>The flow and its trigger are removed. Cancel any active run first. This cannot be undone.</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogClose render={<Button variant="outline" disabled={!!busy} />}>Keep flow</AlertDialogClose>
          <Button variant="destructive" disabled={!!busy || offline} onClick={() => { const target = deleting; if (target) void act(target.definition.id, async () => { await api.delete(target.definition.id); setDeleting(null); }); }}>Delete flow</Button>
        </AlertDialogFooter>
      </AlertDialogPopup>
    </AlertDialog>
  </div>;
}
