import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { Tabs } from "@base-ui/react/tabs";
import { ArrowLeft } from "lucide-react";
import { Agents, events, type Session, type Event as AgentEvent, type PermissionRequest } from "@/mygo";
import { useAgents, removeSession } from "@/stores/agents";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { TerminalPane } from "@/features/terminals/TerminalPane";
import { AgentGlyph, SessionBadge, SessionMeta, ErrorNote, ReplyBox, PermissionCard, SessionDiff } from "./shared";
import { errText, isRemoteProcess, sessionTitle, useAction } from "./format";

const transcriptKinds = ["prompt", "message", "tool", "tool_result", "permission", "decision", "turn_done", "error", "system", "needs_input"];
export function SessionDetail({ session }: { session: Session }) {
  const { setOpenId, upsert } = useAgents();
  const action = useAction();
  const remote = isRemoteProcess(session);
  const [verifiedExit, setVerifiedExit] = useState(false);
  const uncertainExit = remote && session.status === "lost";
  const tabs = remote ? ["transcript"] : session.mode === "terminal" ? ["transcript", "terminal", "diff"] : ["transcript", "diff"];
  return <div className="flex h-full min-h-0 flex-col">
    <header className="flex flex-wrap items-center gap-3 border-b px-5 py-3">
      <Button size="icon-sm" variant="ghost" aria-label="Back to dashboard" onClick={() => setOpenId(null)}><ArrowLeft /></Button>
      <AgentGlyph kind={session.kind} />
      <div className="min-w-0 flex-1"><h2 className="truncate font-medium">{sessionTitle(session)}</h2><SessionMeta session={session} /></div>
      <SessionBadge session={session} />
      {session.live ? <Button size="sm" variant="outline" disabled={action.busy} onClick={() => void action.run(async () => { upsert(await Agents.stop(session.id)); setVerifiedExit(false); })}>{remote ? "Request stop" : "Stop"}</Button> : <>
        {!remote && <Button size="sm" variant="outline" disabled={action.busy} onClick={() => void action.run(async () => upsert(await Agents.restart(session.id, false, "")))}>Resume / Restart</Button>}
        <Button size="sm" variant="outline" disabled={action.busy || (uncertainExit && !verifiedExit)} onClick={() => void action.run(async () => { upsert(await Agents.restart(session.id, true, "")); setVerifiedExit(false); })}>Restart fresh</Button>
        <Button size="sm" variant="destructive-outline" disabled={action.busy} onClick={() => void action.run(async () => { await Agents.remove(session.id); useAgents.setState((state) => ({ sessions: removeSession(state.sessions, session.id), openId: null })); })}>Remove</Button>
      </>}
      {uncertainExit && <label className="flex w-full items-center gap-2 text-xs text-muted-foreground"><Checkbox checked={verifiedExit} onCheckedChange={setVerifiedExit} />I verified that the previous process has stopped on this box before starting another.</label>}
    </header>
    <div className="px-5"><ErrorNote error={action.error || session.error} /></div>
    {remote && <p className="px-5 py-2 text-xs text-muted-foreground">SSH process · output and exit status only. There is no interactive terminal, conversation resume, or local worktree review. Restart fresh reruns the task with its saved model and reasoning.</p>}
    <Tabs.Root defaultValue="transcript" className="flex min-h-0 flex-1 flex-col">
      <Tabs.List aria-label="Session details" className="flex gap-4 border-b px-5">{tabs.map((tab) => <Tabs.Tab key={tab} value={tab} className="border-b-2 border-transparent px-1 py-2 text-sm capitalize text-muted-foreground outline-none data-active:border-primary data-active:text-foreground focus-visible:ring-2 focus-visible:ring-ring">{tab}</Tabs.Tab>)}</Tabs.List>
      <Tabs.Panel value="transcript" className="flex min-h-0 flex-1 flex-col"><Transcript session={session} /></Tabs.Panel>
      {!remote && session.mode === "terminal" && <Tabs.Panel value="terminal" className="flex min-h-0 flex-1 p-3">{session.terminalId ? <TerminalPane id={session.terminalId} active onFocus={() => {}} fontSize={13} /> : <p role="status" className="m-auto text-muted-foreground">This session has no attached terminal.</p>}</Tabs.Panel>}
      {!remote && <Tabs.Panel value="diff" className="flex min-h-0 flex-1 p-3"><SessionDiff session={session} /></Tabs.Panel>}
    </Tabs.Root>
    <p className="shrink-0 border-t px-5 py-2 text-[11px] text-muted-foreground">{remote ? "The daemon tracks this process over SSH. An interrupted connection does not confirm that the remote process stopped." : "Agents are child processes of the daemon: they survive GUI restarts, not daemon exit."}</p>
  </div>;
}
function Transcript({ session }: { session: Session }) {
  const [entries, setEntries] = useState<AgentEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const scroller = useRef<HTMLDivElement>(null);
  const pinned = useRef(true);
  useEffect(() => {
    let active = true; const queued: AgentEvent[] = []; let loaded = false;
    const off = events.agentsEvent.on((ev) => { if (ev.sessionId !== session.id || !transcriptKinds.includes(ev.kind)) return; if (!loaded) queued.push(ev); else setEntries((items) => items.some((x) => x.id === ev.id) ? items : [...items, ev].sort((a, b) => a.id - b.id)); });
    void Agents.transcript(session.id, 500).then((history) => { if (!active) return; const all = new Map([...history, ...queued].map((e) => [e.id, e])); setEntries([...all.values()].sort((a, b) => a.id - b.id)); loaded = true; setLoading(false); }, (e: unknown) => { if (active) { setError(errText(e)); setEntries(queued); loaded = true; setLoading(false); } });
    return () => { active = false; off(); };
  }, [session.id]);
  useLayoutEffect(() => { if (pinned.current && scroller.current) scroller.current.scrollTop = scroller.current.scrollHeight; }, [entries]);
  return <><div ref={scroller} onScroll={() => { const el = scroller.current; if (el) pinned.current = el.scrollHeight - el.scrollTop - el.clientHeight < 48; }} className="min-h-0 flex-1 overflow-auto px-5 py-4"><div className="mx-auto flex max-w-4xl flex-col gap-3"><ErrorNote error={error} />{loading && <p role="status">Loading transcript…</p>}{!loading && !entries.length && <p role="status" className="text-muted-foreground">No output has been recorded yet.</p>}{entries.map((ev) => <TranscriptEntry key={ev.id} event={ev} session={session} />)}</div></div><div className="border-t px-5 py-3"><ReplyBox session={session} /></div></>;
}
function TranscriptEntry({ event: ev, session }: { event: AgentEvent; session: Session }) {
  if (ev.kind === "permission") {
    const data = ev.data as Partial<PermissionRequest> | null;
    const request: PermissionRequest = { id: data?.id ?? String(ev.id), tool: data?.tool ?? ev.text, summary: data?.summary ?? ev.text, at: data?.at ?? ev.at, input: data?.input };
    return <PermissionCard sessionId={session.id} request={request} resolved={!session.pending?.some((r) => r.id === request.id)} />;
  }
  if (ev.kind === "prompt") return <div className="ml-auto max-w-[85%] rounded-lg bg-accent px-3 py-2 text-sm"><p className="mb-1 text-xs font-medium text-muted-foreground">You</p><p className="whitespace-pre-wrap break-words">{ev.text}</p></div>;
  if (ev.kind === "message") return <div className="max-w-[85%] text-sm"><p className="mb-1 text-xs font-medium text-muted-foreground">Agent</p><p className="whitespace-pre-wrap break-words leading-relaxed">{ev.text}</p></div>;
  if (ev.kind === "tool") return <Tooltip><TooltipTrigger render={<p tabIndex={0} className="truncate rounded bg-muted/50 px-2 py-1 font-mono text-xs"  />}>{ev.text}</TooltipTrigger><TooltipPopup><span className="max-w-sm whitespace-pre-wrap break-words">{ev.text}</span></TooltipPopup></Tooltip>;
  if (ev.kind === "error") return <ErrorNote error={ev.text} />;
  return <div className="text-xs text-muted-foreground"><span className="font-medium">{ev.kind.replaceAll("_", " ")}</span>{ev.text && <p className="whitespace-pre-wrap break-words">{ev.text}</p>}</div>;
}
