import { useMemo, useState } from "react";
import { Check, Inbox, RefreshCw } from "lucide-react";
import { Agents, type Session } from "@/mygo";
import { useAgents } from "@/stores/agents";
import { useDaemon } from "@/stores/daemon";
import { useUI } from "@/stores/ui";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Field, FieldLabel } from "@/components/ui/field";
import { Collapsible, CollapsibleTrigger, CollapsiblePanel } from "@/components/ui/collapsible";
import { AlertDialog, AlertDialogPopup, AlertDialogHeader, AlertDialogTitle, AlertDialogDescription, AlertDialogFooter } from "@/components/ui/alert-dialog";
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle, EmptyDescription } from "@/components/ui/empty";
import { NeedsDesktop, NotConnected } from "@/features/shared/States";
import { AgentGlyph, SessionMeta, SessionBadge, SessionDiff, ErrorNote } from "@/features/agents/shared";
import { isRemoteProcess, sessionTitle, ago, useNow, useAction } from "@/features/agents/format";
import { cn } from "@/lib/utils";
import { reviewList } from "./filter";

export function ReviewView() {
  const { sessions, available, offline, error, loaded, loading, refresh } = useAgents();
  const link = useDaemon((s) => s.link);
  const [discarded, setDiscarded] = useState(false);
  const [selected, setSelected] = useState<string | null>(null);
  const entries = useMemo(() => reviewList(Object.values(sessions), discarded), [sessions, discarded]);
  const current = entries.find((s) => s.id === selected) ?? entries[0];
  const now = useNow();
  if (!available) return <NeedsDesktop what="Review" />;
  if (offline || link === "offline") return <div className="flex h-full"><NotConnected app="Review" what={error || "The daemon is offline. Reconnect to review work."} action={{ label: "Retry", onClick: () => { void useDaemon.getState().refresh(); void refresh(); } }} /></div>;
  return <div className="flex h-full min-h-0 flex-col"><header className="flex min-h-12 shrink-0 flex-wrap items-center gap-3 border-b bg-sidebar/40 px-6 py-2"><p className="flex-1 text-xs text-muted-foreground">{entries.length} finished sessions. Review local work or inspect remote output.</p><label className="flex items-center gap-2 text-xs"><Checkbox checked={discarded} onCheckedChange={setDiscarded} />Show discarded</label><Button size="sm" variant="ghost" disabled={loading} onClick={() => void refresh()} aria-label="Refresh review"><RefreshCw /></Button></header>
    {!loaded ? <p role="status" className="p-6">Loading finished work…</p> : !current ? <Empty className="flex-1"><EmptyHeader><EmptyMedia variant="icon"><Inbox /></EmptyMedia><EmptyTitle>Nothing to review</EmptyTitle><EmptyDescription>Finished and failed agent turns appear here. Approve local work, send feedback, or inspect remote output.</EmptyDescription></EmptyHeader></Empty> : <div className="flex min-h-0 flex-1"><aside aria-label="Work to review" className="w-72 shrink-0 overflow-auto border-r p-2 max-lg:w-56"><ul className="flex flex-col gap-1">{entries.map((s) => <li key={s.id}><button type="button" aria-current={current.id === s.id ? "true" : undefined} onClick={() => setSelected(s.id)} className={cn("flex w-full flex-col gap-2 rounded-lg px-3 py-row-pad text-left outline-none hover:bg-accent/50 focus-visible:ring-2 focus-visible:ring-ring", current.id === s.id && "bg-accent")}><span className="flex min-w-0 items-center gap-2"><AgentGlyph kind={s.kind} /><span className="min-w-0 flex-1 truncate text-[13px] font-medium">{sessionTitle(s)}</span></span><span className="text-[11px] text-muted-foreground">{!isRemoteProcess(s) && <>{s.files} files · <span className="font-mono text-success-foreground">+{s.additions}</span> <span className="font-mono text-destructive-foreground">−{s.deletions}</span> · </>}{ago(s.updatedAt, now)}</span><span className="flex flex-wrap gap-1"><CheckBadge session={s} />{s.review && <Badge variant={s.review === "approved" ? "success" : "secondary"}>{s.review === "sent-back" ? "Sent back" : s.review}</Badge>}{s.status === "failed" && <Badge variant="error">Failed</Badge>}</span></button></li>)}</ul></aside><ReviewDetail key={current.id} session={current} /></div>}
  </div>;
}
function CheckBadge({ session }: { session: Session }) {
  if (isRemoteProcess(session)) return <Badge variant="secondary">SSH process</Badge>;
  return <Badge variant={session.checkState === "passing" ? "success" : session.checkState === "failing" ? "error" : "secondary"}>Check: {session.checkState || "none"}</Badge>;
}
function ReviewDetail({ session: s }: { session: Session }) {
  const upsert = useAgents((st) => st.upsert);
  const action = useAction();
  const [sendBack, setSendBack] = useState(false);
  const [note, setNote] = useState("");
  const [discard, setDiscard] = useState(false);
  const [checking, setChecking] = useState(false);
  const [command, setCommand] = useState(s.checkCommand);
  if (isRemoteProcess(s)) return <section className="flex min-w-0 flex-1 flex-col gap-4 overflow-auto p-5">
    <div className="flex items-center gap-2"><AgentGlyph kind={s.kind} /><h3 className="min-w-0 flex-1 truncate text-sm font-medium">{sessionTitle(s)}</h3><SessionBadge session={s} /></div>
    <SessionMeta session={s} />
    <p className="text-sm text-muted-foreground">This task ran as an SSH process. Local diff, check, approval, send-back and worktree discard actions are unavailable. Inspect its output and the files on its box instead.</p>
    <ErrorNote error={s.error || null} />
    <p className="whitespace-pre-wrap break-words text-sm">{s.lastMessage || s.result || "No final output was recorded."}</p>
    <Button className="self-start" size="sm" variant="outline" onClick={() => { useAgents.getState().setOpenId(s.id); useUI.getState().setView("agents"); }}>Inspect session</Button>
  </section>;
  return <section className="flex min-h-0 min-w-0 flex-1 flex-col overflow-auto"><header className="flex flex-col gap-3 border-b px-5 py-4"><div className="flex items-center gap-2"><AgentGlyph kind={s.kind} /><h3 className="min-w-0 flex-1 truncate text-sm font-medium">{sessionTitle(s)}</h3><SessionBadge session={s} /></div><SessionMeta session={s} /><div className="flex flex-wrap gap-2"><Button size="sm" disabled={action.busy || s.review === "approved" || s.review === "discarded"} onClick={() => void action.run(async () => upsert(await Agents.review(s.id, "approve", "")))}><Check />Approve</Button><Button size="sm" variant="outline" disabled={action.busy || s.review === "discarded"} onClick={() => setSendBack((v) => !v)}>Send back…</Button><Button size="sm" variant="destructive-outline" disabled={action.busy || s.review === "discarded"} onClick={() => setDiscard(true)}>Discard</Button><Button size="sm" variant="ghost" disabled={action.busy || s.checkState === "running"} onClick={() => setChecking((v) => !v)}>Run check…</Button></div><ErrorNote error={action.error || s.error} />
    {sendBack && <form className="flex flex-col gap-2" onSubmit={(e) => { e.preventDefault(); void action.run(async () => { upsert(await Agents.review(s.id, "send-back", note)); setSendBack(false); setNote(""); }); }}><Field><FieldLabel>What should change?</FieldLabel><Textarea required value={note} onChange={(e) => setNote(e.target.value)} /></Field><Button size="sm" type="submit" disabled={action.busy || !note.trim()}>Send feedback</Button></form>}
    {checking && <form className="flex items-end gap-2" onSubmit={(e) => { e.preventDefault(); void action.run(async () => { upsert(await Agents.check(s.id, command)); setChecking(false); }); }}><Field className="flex-1"><FieldLabel>Check command</FieldLabel><Input required value={command} onChange={(e) => setCommand(e.target.value)} placeholder="bun run test" /></Field><Button size="sm" type="submit" disabled={action.busy || s.checkState === "running" || !command.trim()}>Run check</Button></form>}
    {s.checkState && <Collapsible><CollapsibleTrigger className="flex items-center gap-2 text-xs"><CheckBadge session={s} /><span>Show check output</span></CollapsibleTrigger><CollapsiblePanel><pre className="mt-2 max-h-48 overflow-auto whitespace-pre-wrap rounded bg-muted p-3 text-xs">{s.checkCommand}{"\n"}{s.checkOutput || (s.checkState === "running" ? "Check is running…" : "No output.")}</pre></CollapsiblePanel></Collapsible>}
    </header><div className="flex flex-col gap-2 px-5 py-4"><h4 className="text-xs font-medium">Last message</h4><p className="max-h-52 overflow-auto whitespace-pre-wrap break-words text-sm leading-relaxed text-muted-foreground">{s.lastMessage || s.result || s.error || "No final message was reported."}</p>{s.reviewNote && <p className="whitespace-pre-wrap text-xs text-muted-foreground">Review note: {s.reviewNote}</p>}</div><div className="flex min-h-80 flex-1 flex-col px-5 pb-5"><h4 className="mb-2 text-xs font-medium">Changed files</h4><SessionDiff session={s} /></div>
    <AlertDialog open={discard} onOpenChange={(o) => { if (!action.busy) setDiscard(o); }}><AlertDialogPopup><AlertDialogHeader><AlertDialogTitle>Discard this work?</AlertDialogTitle><AlertDialogDescription>The agent will be stopped. A worktree created by the daemon for this session will be removed. This cannot be undone.</AlertDialogDescription></AlertDialogHeader><div className="px-6"><ErrorNote error={action.error} /></div><AlertDialogFooter><Button variant="outline" disabled={action.busy} onClick={() => setDiscard(false)}>Cancel</Button><Button variant="destructive" disabled={action.busy} onClick={() => void action.run(async () => { upsert(await Agents.review(s.id, "discard", "")); setDiscard(false); })}>Discard work</Button></AlertDialogFooter></AlertDialogPopup></AlertDialog>
  </section>;
}
