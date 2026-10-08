import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { useEffect, useId, useState } from "react";
import { Check, Circle, LoaderCircle } from "lucide-react";
import { Agents, type Session, type PermissionRequest, type ReviewDiff } from "@/mygo";
import { useAgents } from "@/stores/agents";
import { useBoxes } from "@/stores/boxes";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { DiffView } from "@/components/diff/DiffView";
import { toDiffFiles } from "@/lib/diff";
import { cn } from "@/lib/utils";
import { duration, folderOf, kindLabel, useAction, useNow, errText, isRemoteProcess } from "./format";
import { ProviderLogo } from "./ProviderLogo";

export function AgentGlyph({ kind }: { kind: string }) {
  return <Tooltip><TooltipTrigger render={<span tabIndex={0} role="img" aria-label={kindLabel(kind)} className="inline-flex size-6 shrink-0 items-center justify-center" />}><ProviderLogo kind={kind} /></TooltipTrigger><TooltipPopup><span className="max-w-sm whitespace-pre-wrap break-words">{kindLabel(kind)}</span></TooltipPopup></Tooltip>;
}
export function StateGlyph({ status }: { status: string }) {
  const Icon = status === "working" ? LoaderCircle : status === "done" ? Check : Circle;
  return <Icon aria-hidden className={cn("size-3", status === "working" ? "animate-spin text-info motion-reduce:animate-none" : status === "done" ? "text-success" : status === "needs-you" ? "fill-warning text-warning" : "text-muted-foreground")} />;
}
export function SessionMeta({ session: s }: { session: Session }) {
  const now = useNow();
  const machine = useBoxes((state) => state.machines.find((item) => item.id === (s.machine || "local"))?.name);
  return <Tooltip><TooltipTrigger render={<p tabIndex={0} className="truncate text-[11px] text-muted-foreground"  />}>{[machine ?? s.machine, folderOf(s), s.branch, s.model, s.reasoning ? `${s.reasoning} reasoning` : "", duration(now - Date.parse(s.startedAt))].filter(Boolean).join(" · ")}</TooltipTrigger><TooltipPopup><span className="max-w-sm whitespace-pre-wrap break-words">{s.dir}</span></TooltipPopup></Tooltip>;
}
export function SessionBadge({ session: s }: { session: Session }) {
  return <Badge variant={s.status === "failed" ? "error" : s.review === "approved" ? "success" : "secondary"}>{s.review === "approved" ? "Approved" : s.status === "needs-you" ? "Needs you" : s.status}</Badge>;
}
export function ErrorNote({ error }: { error: string | null }) {
  return error ? <p role="alert" className="whitespace-pre-wrap break-words text-xs text-destructive-foreground">{error}</p> : null;
}
function approvalText(value: string): string {
  return value.replace(/[\u0000-\u001f\u007f-\u009f\u061c\u200b-\u200f\u2028-\u202e\u2060-\u206f\ufeff]/g,
    (character) => `\\u${character.charCodeAt(0).toString(16).padStart(4, "0")}`);
}

export function PermissionCard({ sessionId, request, resolved = false }: { sessionId: string; request: PermissionRequest; resolved?: boolean }) {
  const action = useAction();
  const reviewId = useId();
  const [reviewed, setReviewed] = useState("");
  const complete = !!request.inputJSON;
  return <div className="flex min-w-0 flex-col gap-2 rounded-md border border-warning/30 bg-warning/5 p-2 text-xs">
    <div className="flex items-center justify-between gap-2"><strong className="break-all font-mono">{approvalText(JSON.stringify(request.tool))}</strong><span className="shrink-0 text-muted-foreground">{resolved ? "Resolved" : "Permission requested"}</span></div>
    <p className="text-muted-foreground">Complete tool arguments. Control and directional characters are shown as escapes; this is untrusted agent input.</p>
    {complete ? <pre aria-label="Complete tool arguments" tabIndex={0} className="selectable max-h-72 overflow-auto whitespace-pre-wrap break-all rounded border bg-background p-2 font-mono">{approvalText(request.inputJSON)}</pre>
      : <p role="alert">The complete input is unavailable. This request cannot be approved.</p>}
    {!resolved && <>
      <div className="flex items-start gap-2"><Checkbox id={reviewId} checked={reviewed === request.id} disabled={!complete || action.busy} onCheckedChange={(checked) => setReviewed(checked ? request.id : "")} /><label htmlFor={reviewId}>I reviewed the complete tool name and arguments.</label></div>
      <div className="flex gap-1"><Button size="xs" disabled={!complete || reviewed !== request.id || action.busy} onClick={() => void action.run(async () => { await Agents.approve(sessionId, request.id, true, ""); useAgents.getState().upsert(await Agents.get(sessionId)); })}>Allow</Button><Button size="xs" variant="outline" disabled={action.busy} onClick={() => void action.run(async () => { await Agents.approve(sessionId, request.id, false, ""); useAgents.getState().upsert(await Agents.get(sessionId)); })}>Deny</Button></div>
    </>}
    <ErrorNote error={action.error} />
  </div>;
}
export function ReplyBox({ session, onSent }: { session: Session; onSent?: () => void }) {
  const [text, setText] = useState("");
  const action = useAction();
  const remote = isRemoteProcess(session);
  if (remote && session.status === "lost") return <p className="text-xs text-muted-foreground">The remote process exit is unconfirmed. Inspect the box and verify that the previous process stopped before starting a new task.</p>;
  if (session.mode === "terminal" && session.live) return <p className="text-xs text-muted-foreground">Open the task's Terminal sheet to continue. This session's foreground process owns its input.</p>;
  if (remote && session.live) return <p className="text-xs text-muted-foreground">This SSH process reports output and exit status. It cannot receive messages while running.</p>;
  return <form className="flex flex-col gap-2" onSubmit={(e) => { e.preventDefault(); if (!text.trim()) return; void action.run(async () => { useAgents.getState().upsert(await Agents.prompt(session.id, text)); setText(""); onSent?.(); }); }}>
    {remote && <p className="text-xs text-muted-foreground">Start a fresh task on the same box with this session's model and reasoning. This does not resume the previous conversation.</p>}
    <div className="flex gap-2"><Input aria-label={remote ? "New task on this box" : "Reply to agent"} placeholder={remote ? "New task on this box…" : session.status === "lost" || session.status === "stopped" ? "Resume with a message…" : "Reply to agent…"} value={text} disabled={action.busy} onChange={(e) => setText(e.target.value)} /><Button size="sm" type="submit" disabled={action.busy || !text.trim()}>{remote ? "Start task" : "Send"}</Button></div><ErrorNote error={action.error} />
  </form>;
}
export function SessionDiff({ session }: { session: Session }) {
  const [result, setResult] = useState<{ key: string; diff: ReviewDiff | null; error: string | null } | null>(null);
  const [version, setVersion] = useState(0);
  const key = `${session.id}:${session.updatedAt}:${version}`;
  const remote = isRemoteProcess(session);
  useEffect(() => {
    if (remote) return;
    let active = true;
    void Agents.diff(session.id).then(
      (diff) => { if (active) setResult({ key, diff, error: null }); },
      (e: unknown) => { if (active) setResult({ key, diff: null, error: errText(e) }); },
    );
    return () => { active = false; };
  }, [session.id, key, remote]);
  const diff = result?.key === key ? result.diff : null;
  const error = result?.key === key ? result.error : null;
  if (remote) return <p className="p-4 text-sm text-muted-foreground">Worktree diffs and local review are unavailable for SSH process sessions. Inspect the transcript or files on the selected box.</p>;
  if (error || diff?.error) return <div className="flex flex-col gap-2 p-4"><ErrorNote error={error || diff?.error || null} /><Button variant="outline" size="sm" onClick={() => setVersion((v) => v + 1)}>Retry diff</Button></div>;
  if (!diff) return <p role="status" className="p-4 text-muted-foreground">Loading changes…</p>;
  return <DiffView className="min-h-64 flex-1" files={toDiffFiles(diff.diff)} truncated={diff.truncated || diff.diff.truncated} />;
}
