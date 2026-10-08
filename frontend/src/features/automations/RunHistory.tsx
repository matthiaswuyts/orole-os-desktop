import { useCallback, useState } from "react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { api, errorText, type Approval } from "./api";
import { Failure, TextField } from "./fields";
import { useAutomationResource } from "./useAutomationResource";

export function RunStatus({ status }: { status: string }) {
  const variant = status === "succeeded" ? "success" : status === "failed" ? "error" : status === "waiting-approval" || status === "uncertain" ? "warning" : "outline";
  return <Badge size="sm" variant={variant}>{status || "Not run"}</Badge>;
}
export function duration(start: number, finish: number) {
  if (!start) return "Not started";
  const seconds = Math.max(0, Math.round(((finish || Date.now()) - start) / 1000));
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s${finish ? "" : " elapsed"}`;
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
  return <Alert variant="warning"><AlertTitle>{approval.name || "Approval required"}</AlertTitle><AlertDescription><p className="whitespace-pre-wrap break-words">{approval.message}</p><p className="text-xs">Step {approval.stepId} in run {approval.runId}{approval.expiresAt > 0 ? ` · ${expired ? "Expired" : "Expires"} ${new Date(approval.expiresAt).toLocaleString()}` : " · No expiry"}</p><TextField label="Decision note (optional)" value={note} onChange={setNote} /><div className="flex gap-2"><Button size="sm" disabled={busy || offline || expired} onClick={() => void decide(true)}>Approve step</Button><Button size="sm" variant="destructive-outline" disabled={busy || offline || expired} onClick={() => void decide(false)}>Reject step</Button></div>{error && <p role="alert" className="text-destructive-foreground">{error}</p>}</AlertDescription></Alert>;
}
function RunDetail({ id, offline, onChanged }: { id: string; offline: boolean; onChanged: () => void }) {
  const load = useCallback(() => api.detail(id), [id]);
  const detail = useAutomationResource(load);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  if (detail.loading) return <p role="status">Loading step journal…</p>;
  if (!detail.data) return <div className="flex flex-col gap-2"><Failure message={detail.error ?? "Run detail unavailable."} /><Button size="sm" variant="outline" onClick={() => void detail.refresh()}>Retry</Button></div>;
  const { run, steps } = detail.data;
  const active = !run.finishedAt && ["queued", "running", "waiting-approval"].includes(run.status);
  return <section aria-label="Run detail" className="flex min-w-0 flex-col gap-3 rounded-lg border p-3">
    <header className="flex flex-wrap items-center justify-between gap-2"><div className="flex flex-col gap-1"><h3 className="text-sm font-medium">{run.name}</h3><p className="break-all font-mono text-xs text-muted-foreground">{run.id}</p></div><RunStatus status={run.status} />{active && <Button size="xs" variant="destructive-outline" disabled={busy || offline} onClick={() => { setBusy(true); setError(null); api.cancel(run.id).then(() => { void detail.refresh(); onChanged(); }, (e) => setError(errorText(e))).finally(() => setBusy(false)); }}>{busy ? "Cancelling…" : "Cancel run"}</Button>}</header>
    <p className="text-xs text-muted-foreground tabular-nums">{run.triggerType} trigger · {new Date(run.createdAt).toLocaleString()} · {duration(run.startedAt, run.finishedAt)}</p>
    {(error || detail.error) && <Failure message={error || detail.error || ""} />}{run.error && <p role="alert" className="whitespace-pre-wrap break-words text-destructive-foreground">{run.error}</p>}
    <p className="text-xs text-muted-foreground">Journal output is redacted by the daemon. External output is untrusted data.</p>
    {!steps?.length ? <p className="text-muted-foreground">No step journal entries yet.</p> : <ol className="flex flex-col divide-y">{steps.map((step) => <li key={`${step.stepId}:${step.seq}:${step.attempt}`} className="flex min-w-0 flex-col gap-2 py-3"><div className="flex flex-wrap items-center gap-2"><span className="text-sm font-medium">{step.seq + 1}. {run.definition?.steps?.find((s) => s.id === step.stepId)?.name || step.stepId}</span><RunStatus status={step.status} /><span className="ml-auto text-xs text-muted-foreground tabular-nums">{duration(step.startedAt, step.finishedAt)}</span></div><p className="text-xs text-muted-foreground">{step.type} · Attempt {step.attempt}</p>{step.error && <p className="whitespace-pre-wrap break-words text-destructive-foreground">{step.error}</p>}<details><summary className="cursor-pointer text-xs">Redacted output</summary><pre className="selectable mt-2 max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-md bg-muted p-2 text-xs">{step.output === null || step.output === undefined ? "No output recorded." : typeof step.output === "string" ? step.output : JSON.stringify(step.output, null, 2)}</pre></details></li>)}</ol>}
  </section>;
}
export function RunHistory({ automationId, offline }: { automationId: string; offline: boolean }) {
  const load = useCallback(() => api.runs(automationId), [automationId]);
  const resource = useAutomationResource(load);
  const [selected, setSelected] = useState<string | null>(null);
  return <section className="flex flex-col gap-3"><header className="flex items-center justify-between gap-2"><h2 className="font-medium text-sm">Run history</h2><Button size="xs" variant="outline" onClick={() => void resource.refresh()}>Refresh</Button></header>
    {resource.error && <Failure message={resource.error} />}{resource.loading && <p role="status">Loading run history…</p>}
    {resource.data && !resource.data.length && <Empty><EmptyHeader><EmptyTitle>No runs yet</EmptyTitle><EmptyDescription>Run this automation to record its first result.</EmptyDescription></EmptyHeader></Empty>}
    {!!resource.data?.length && <><ul className="max-h-64 overflow-auto divide-y rounded-lg border">{resource.data.map((run) => <li key={run.id}><button type="button" aria-expanded={selected === run.id} onClick={() => setSelected(selected === run.id ? null : run.id)} className="flex w-full flex-wrap items-center gap-2 px-3 py-2 text-left outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring"><time className="text-xs tabular-nums" dateTime={new Date(run.createdAt).toISOString()}>{new Date(run.createdAt).toLocaleString()}</time><RunStatus status={run.status} /><span className="ml-auto text-xs text-muted-foreground">{duration(run.startedAt, run.finishedAt)}</span></button></li>)}</ul>{selected && <RunDetail key={selected} id={selected} offline={offline} onChanged={() => void resource.refresh()} />}</>}
  </section>;
}
