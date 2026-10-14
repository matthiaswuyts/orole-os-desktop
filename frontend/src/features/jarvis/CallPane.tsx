import { ChevronLeftIcon, CodeIcon, UsersIcon, WrenchIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { errText } from "@/features/agents/format";
import { Bubble } from "@/features/chat/parts/Bubble";
import { UserBubble } from "@/features/chat/parts/MessageBubble";
import { isActive, messageText, stepState, toolLabel } from "@/features/chat/parts/format";
import { inDesktop } from "@/lib/backend";
import { events, Jarvis, type CallRecord, type Message, type MsgPart } from "@/mygo";
import { useChat } from "@/stores/chat";
import { applyDelta, upsert } from "@/stores/chatMessages";
import { callLook, callPart, clock } from "./callFormat";

const STATUS: Record<string, string> = {
  queued: "Queued", dialing: "Dialing", in_progress: "On the line", completed: "Ended", no_answer: "No answer",
  voicemail: "Voicemail", failed: "Failed", rejected: "Declined",
};

/** The call's start as a short local date and time. */
const startedAt = (call: CallRecord) => new Date(call.startedAt || call.createdAt).toLocaleString([], { dateStyle: "medium", timeStyle: "short" });

/**
 * One phone call beside the conversation: who rang, when, how long, the estimated cost and its status, then the spoken
 * transcript (the owner's lines and the bot's, with what the bot did while talking). The spoken turns live in the bot's
 * history but not in the thread; they come from jarvis.calls.transcript, and while the call is on, from the
 * chat:call-message / chat:call-delta events. The header refreshes when the call's card in the chat changes.
 */
export function CallPane({ callId, botId, onClose }: { callId: string; botId: string; onClose: () => void }) {
  const botName = useChat((s) => s.bots.find((b) => b.id === botId)?.name ?? "Bot");
  const [call, setCall] = useState<CallRecord | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [error, setError] = useState<string | null>(null);
  // Messages an event delivered: a transcript response that started earlier must not overwrite them.
  const live = useRef(new Map<string, Message>());

  useEffect(() => {
    if (!inDesktop) return;
    // The side pane remounts this for another call, so the state starts empty for each call id.
    let current = true;
    const load = () => {
      Jarvis.transcript(callId).then((t) => {
        if (!current) return;
        setCall(t.call);
        setMessages(Array.from(live.current.values()).reduce(upsert, t.messages));
      }, (failure: unknown) => { if (current) setError(errText(failure)); });
    };
    // Subscribe before fetching so nothing said in between is lost.
    const offMessage = events.chatCallMessage.on((message) => {
      if (message.callId !== callId) return;
      live.current.set(message.id, message);
      setMessages((list) => upsert(list, message));
    });
    const offDelta = events.chatCallDelta.on((delta) => {
      setMessages((list) => {
        const at = list.findIndex((m) => m.id === delta.messageId);
        const next = at >= 0 ? applyDelta(list[at]!, delta) : null;
        if (!next) return list;
        live.current.set(next.id, next);
        const copy = list.slice();
        copy[at] = next;
        return copy;
      });
    });
    const offCard = events.chatMessage.on((message) => {
      if (message.parts.some((part) => part.type === "call" && part.id === callId)) load();
    });
    load();
    return () => { current = false; offMessage(); offDelta(); offCard(); };
  }, [callId]);

  const look = call ? callLook(callPart(call)) : null;
  const durationMs = call?.answeredAt && call.endedAt ? call.endedAt - call.answeredAt : 0;
  return <section aria-labelledby="side-pane-title" data-slot="call-pane" className="flex h-full min-w-0 flex-col">
    <header className="flex shrink-0 items-start gap-2 border-b px-3 py-2">
      <Button type="button" variant="ghost" size="icon-xs" aria-label="Close call pane" onClick={onClose}><ChevronLeftIcon aria-hidden /></Button>
      {look && <look.Icon aria-hidden className={look.missed ? "mt-0.5 size-4 shrink-0 text-destructive-foreground" : "mt-0.5 size-4 shrink-0 text-muted-foreground"} />}
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <h2 id="side-pane-title" tabIndex={-1} className="truncate text-sm font-medium outline-none">{look ? `${look.label} · ${botName}` : "Phone call"}</h2>
        {call && <dl data-slot="call-meta" className="flex flex-wrap gap-x-3 gap-y-0.5 text-xs text-muted-foreground tabular-nums">
          <div className="flex gap-1"><dt className="sr-only">Started</dt><dd>{startedAt(call)}</dd></div>
          <div className="flex gap-1"><dt>Duration</dt><dd>{durationMs > 0 ? clock(durationMs) : "–"}</dd></div>
          <div className="flex gap-1"><dt>Cost</dt><dd>{call.estCostMicros > 0 ? `est. $${(call.estCostMicros / 1e6).toFixed(2)}` : "–"}</dd></div>
          <div className="flex gap-1"><dt className="sr-only">Status</dt><dd>{STATUS[call.status] ?? call.status}</dd></div>
        </dl>}
        {call?.reason && <p className="selectable break-words text-xs text-muted-foreground">{call.reason}</p>}
      </div>
    </header>
    <div className="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto p-3" aria-live="polite">
      {error ? <p role="alert" className="m-auto p-6 text-center text-sm text-muted-foreground">{error}</p>
        : !call ? <p role="status" className="m-auto p-6 text-center text-sm text-muted-foreground">Loading the call…</p>
          : messages.length === 0 ? <p className="m-auto p-6 text-center text-sm text-muted-foreground">{look?.live ? "Nothing has been said yet." : "No transcript for this call."}</p>
            : <ol aria-label="Transcript" className="flex flex-col gap-2">
              {messages.map((message) => <li key={message.id} className="flex min-w-0 flex-col gap-1">
                <Turn message={message} botName={botName} />
              </li>)}
            </ol>}
    </div>
  </section>;
}

/** One spoken turn: the owner's line, or the bot's line with what it did while answering. */
function Turn({ message, botName }: { message: Message; botName: string }) {
  if (message.role === "user") {
    const text = messageText(message);
    return text ? <UserBubble><span className="sr-only">You: </span><span className="selectable whitespace-pre-wrap">{text}</span></UserBubble> : null;
  }
  const active = isActive(message.status);
  const rows = message.parts.map((part, i) => <Activity key={i} part={part} active={active} botName={botName} />);
  return <>
    {rows}
    {active && !messageText(message) && <span className="px-1 text-xs text-muted-foreground cv-shimmer">{botName} is answering…</span>}
    {message.status === "error" && <span className="px-1 text-xs text-destructive-foreground">{message.error || "The reply failed."}</span>}
    {message.status === "canceled" && <span className="px-1 text-xs text-muted-foreground">Interrupted</span>}
  </>;
}

/** A part of a bot turn: spoken text as a bubble, tool, task and delegation activity as one compact line. */
function Activity({ part, active, botName }: { part: MsgPart; active: boolean; botName: string }) {
  const row = "flex min-w-0 items-center gap-1.5 px-1 text-xs text-muted-foreground";
  switch (part.type) {
    case "text":
      return part.text && !part.hidden ? <Bubble><span className="sr-only">{botName}: </span><span className="selectable whitespace-pre-wrap">{part.text}</span></Bubble> : null;
    case "tool":
      return <span data-slot="call-tool" className={row}><WrenchIcon aria-hidden className="size-3 shrink-0" /><span className="truncate">{toolLabel(part.name, stepState(part, active))}</span></span>;
    case "delegation":
      return <span className={row}><UsersIcon aria-hidden className="size-3 shrink-0" /><span className="truncate">Asked {part.name || "another bot"}</span></span>;
    case "task":
      return part.id
        ? <button type="button" data-slot="call-task" onClick={() => useChat.getState().openTaskPane(part.id!)}
          className={`${row} w-fit rounded-md outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring`}>
          <CodeIcon aria-hidden className="size-3 shrink-0" /><span className="truncate">Code task: {part.title || "Code task"}</span>
        </button>
        : <span className={row}><CodeIcon aria-hidden className="size-3 shrink-0" /><span className="truncate">Code task: {part.title || "Code task"}</span></span>;
    default:
      return null;
  }
}
