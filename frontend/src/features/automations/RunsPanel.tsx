import { useCallback, useState } from "react";
import { ExternalLink, MessageSquare, RefreshCw, Split } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { openTask } from "@/features/projects/tasks";
import { useChat } from "@/stores/chat";
import { cn } from "@/lib/utils";
import { api, errorText, type Approval, type StepType } from "./api";
import { Failure, TextField } from "./fields";
import { LANE_LABEL, duration, params, relativeTime, timeline, type TimelineRow } from "./flow";
import { BlockIcon } from "./BlockIcon";
import { useAutomationResource } from "./useAutomationResource";

const ACTIVE = ["queued", "running", "waiting-approval"];

export function RunStatus({ status }: { status: string }) {
  const variant = status === "succeeded" ? "success" : status === "failed" ? "error" : status === "waiting-approval" || status === "uncertain" ? "warning" : status === "running" ? "info" : "outline";
  return <Badge size="sm" variant={variant}>{status || "Not run"}</Badge>;
}

export function ApprovalPrompt({ approval, offline, onResolved }: { approval: Approval; offline: boolean; onResolved: () => void }) {
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const decide = async (approved: boolean) => {
    setBusy(true); setError(null);
    try { await api.approve(approval.runId, approval.stepId, approved, note); onResolved(); }
    catch (e) { setError(errorText(e)); }
    finally { setBusy(false); }
  };
  const expired = approval.expiresAt > 0 && approval.expiresAt <= Date.now();
  return <Alert variant="warning">
    <AlertTitle>{approval.name || "Approval required"}</AlertTitle>
    <AlertDescription>
      <p className="whitespace-pre-wrap break-words">{approval.message}</p>
      <p className="text-xs">Block {approval.stepId}{approval.expiresAt > 0 ? ` · ${expired ? "Expired" : "Expires"} ${relativeTime(approval.expiresAt)}` : ""}</p>
      <TextField label="Note (optional)" value={note} onChange={setNote} />
      <div className="flex gap-2">
        <Button size="xs" disabled={busy || offline || expired} onClick={() => void decide(true)}>Approve</Button>
        <Button size="xs" variant="destructive-outline" disabled={busy || offline || expired} onClick={() => void decide(false)}>Reject</Button>
      </div>
      {error && <p role="alert" className="text-destructive-foreground">{error}</p>}
    </AlertDescription>
  </Alert>;
}

function Row({ row, types }: { row: TimelineRow; types: StepType[] }) {
  const { entry, step, depth, lane } = row;
  const spec = types.find((t) => t.type === step?.type);
  const output = entry.output && typeof entry.output === "object" ? entry.output as Record<string, unknown> : null;
  const input = entry.input && typeof entry.input === "object" ? entry.input as Record<string, unknown> : {};
  const taskId = (step?.type === "task.start" || step?.type === "task.wait" || step?.type === "task.prompt") && typeof output?.id === "string" ? output.id : null;
  const botId = step?.type === "bot.tell" ? String(input.botId ?? params(step).botId ?? "") : "";
  return <li className="flex min-w-0 flex-col gap-1 py-2" style={{ paddingLeft: depth * 16 }}>
    <div className="flex min-w-0 items-center gap-2">
      <BlockIcon type={step?.type ?? entry.type} className="size-3.5 shrink-0 text-muted-foreground" />
      <span className="min-w-0 truncate text-sm font-medium">{step?.name || spec?.label || entry.type}</span>
      <span className="shrink-0 font-mono text-[11px] text-muted-foreground">{entry.stepId}</span>
      <RunStatus status={entry.status} />
      {entry.attempt > 1 && <span className="text-xs text-muted-foreground">Attempt {entry.attempt}</span>}
      <span className="ml-auto shrink-0 text-xs text-muted-foreground tabular-nums">{duration(entry.startedAt, entry.finishedAt)}</span>
    </div>
    {lane && <p className="flex items-center gap-1 text-xs text-muted-foreground"><Split aria-hidden className="size-3" />Took the “{LANE_LABEL[lane]}” lane{step?.[lane]?.length ? "" : " (empty)"}</p>}
    {entry.error && <p className="whitespace-pre-wrap break-words text-xs text-destructive-foreground">{entry.error}</p>}
    {(taskId || botId) && <div className="flex flex-wrap gap-1.5">
      {taskId && <Button size="xs" variant="outline" onClick={() => openTask(taskId)}><ExternalLink data-icon="inline-start" />Open task</Button>}
      {botId && !botId.includes("{{") && <Button size="xs" variant="outline" onClick={() => useChat.getState().open(botId)}><MessageSquare data-icon="inline-start" />Open conversation</Button>}
    </div>}
    {entry.output != null && <details className="text-xs">
      <summary className="cursor-pointer rounded-sm text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring">Output</summary>
      <pre className="selectable mt-1 max-h-64 overflow-auto rounded-md border bg-muted/40 p-2 font-mono text-[11px] whitespace-pre-wrap break-words">{JSON.stringify(entry.output, null, 2)}</pre>
    </details>}
  </li>;
}

function RunDetail({ id, types, offline, onChanged }: { id: string; types: StepType[]; offline: boolean; onChanged: () => void }) {
  const load = useCallback(() => api.detail(id), [id]);
  const detail = useAutomationResource(load);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  if (detail.loading) return <p role="status" className="p-3 text-sm text-muted-foreground">Loading run…</p>;
  if (!detail.data) return <div className="flex flex-col items-start gap-2 p-3"><Failure message={detail.error ?? "Run detail unavailable."} /><Button size="xs" variant="outline" onClick={() => void detail.refresh()}>Retry</Button></div>;
  const { run, steps } = detail.data;
  const rows = timeline(run.definition, steps ?? []);
  const active = !run.finishedAt && ACTIVE.includes(run.status);
  const cancel = () => {
    setBusy(true); setError(null);
    api.cancel(run.id).then(() => { void detail.refresh(); onChanged(); }, (e) => setError(errorText(e))).finally(() => setBusy(false));
  };
  return <section aria-label="Run detail" className="flex min-w-0 flex-col gap-2 p-3">
    <header className="flex flex-wrap items-center gap-2">
      <RunStatus status={run.status} />
      <span className="text-xs text-muted-foreground tabular-nums">{run.triggerType} · <time dateTime={new Date(run.createdAt).toISOString()} title={new Date(run.createdAt).toLocaleString()}>{relativeTime(run.createdAt)}</time> · {duration(run.startedAt, run.finishedAt)}</span>
      {active && <Button size="xs" variant="destructive-outline" className="ml-auto" disabled={busy || offline} onClick={cancel}>{busy ? "Cancelling…" : "Cancel run"}</Button>}
    </header>
    {(error || detail.error) && <Failure message={error || detail.error || ""} />}
    {run.error && <p role="alert" className="whitespace-pre-wrap break-words text-sm text-destructive-foreground">{run.error}</p>}
    <p className="text-xs text-muted-foreground">Outputs are redacted by the daemon. Text from outside Orole is untrusted data.</p>
    {!rows.length ? <p className="text-sm text-muted-foreground">No blocks have run yet.</p> : <ol className="flex flex-col divide-y">{rows.map((row) => <Row key={`${row.entry.stepId}:${row.entry.seq}:${row.entry.attempt}`} row={row} types={types} />)}</ol>}
  </section>;
}

/** Runs of one flow and the block timeline of the selected run, refreshed while visible. */
export function RunsPanel({ automationId, types, offline, focus }: { automationId: string; types: StepType[]; offline: boolean; focus: string | null }) {
  const load = useCallback(() => api.runs(automationId), [automationId]);
  const runs = useAutomationResource(load);
  const [picked, setPicked] = useState<string | null>(focus);
  const list = runs.data ?? [];
  const selected = picked ?? list[0]?.id ?? null;
  return <div className="flex min-h-0 flex-1 flex-col md:flex-row">
    <aside aria-label="Runs" className="flex max-h-64 shrink-0 flex-col border-b md:max-h-none md:w-72 md:border-r md:border-b-0">
      <header className="flex items-center justify-between gap-2 px-3 py-2">
        <h2 className="text-sm font-medium">Runs</h2>
        <Button size="icon-xs" variant="ghost" aria-label="Refresh runs" onClick={() => void runs.refresh()}><RefreshCw /></Button>
      </header>
      <div className="min-h-0 flex-1 overflow-auto px-2 pb-2">
        {runs.error && <Failure message={runs.error} />}
        {runs.loading && <p role="status" className="px-1 text-xs text-muted-foreground">Loading runs…</p>}
        {runs.data && !list.length && <Empty><EmptyHeader><EmptyTitle>No runs yet</EmptyTitle><EmptyDescription>Run the flow or wait for its trigger.</EmptyDescription></EmptyHeader></Empty>}
        <ul className="flex flex-col gap-0.5">{list.map((run) => <li key={run.id}>
          <button type="button" aria-current={selected === run.id || undefined} onClick={() => setPicked(run.id)}
            className={cn("flex w-full flex-col gap-0.5 rounded-md px-2 py-1.5 text-left outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring", selected === run.id && "bg-accent")}>
            <span className="flex items-center gap-2"><RunStatus status={run.status} /><span className="truncate text-xs text-muted-foreground">{run.triggerType}</span><span className="ml-auto text-xs text-muted-foreground tabular-nums">{duration(run.startedAt, run.finishedAt)}</span></span>
            <time className="text-xs text-muted-foreground tabular-nums" dateTime={new Date(run.createdAt).toISOString()}>{new Date(run.createdAt).toLocaleString()}</time>
          </button>
        </li>)}</ul>
      </div>
    </aside>
    <div className="min-h-0 min-w-0 flex-1 overflow-auto">
      {selected ? <RunDetail key={selected} id={selected} types={types} offline={offline} onChanged={() => void runs.refresh()} /> : !runs.loading && <p className="p-3 text-sm text-muted-foreground">Choose a run to see what each block did.</p>}
    </div>
  </div>;
}
