import { ChevronLeftIcon, CodeIcon, MessageSquareIcon, UsersIcon, WrenchIcon } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { Markdown } from "@/components/Markdown";
import { Button } from "@/components/ui/button";
import { errText } from "@/features/agents/format";
import { Bubble } from "@/features/chat/parts/Bubble";
import { ChatMessageView, UserBubble } from "@/features/chat/parts/MessageBubble";
import { isActive, messageText, stepState, toolLabel } from "@/features/chat/parts/format";
import { inDesktop } from "@/lib/backend";
import { events, Jarvis, type Bot, type CallRecord, type Message, type MsgPart } from "@/mygo";
import { useChat } from "@/stores/chat";
import { applyDelta, upsert } from "@/stores/chatMessages";
import { callLook, callPart, clock, costText } from "./callFormat";

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
 * chat:call-message / chat:call-delta events. The follow-ups of work started on the call (a report from a bot, a code
 * task's update and the bot's written answer to it) join the same thread, also after the call ended: they arrive through
 * the same events and the header keeps saying how the call ended. The header refreshes when the call's card in the chat
 * changes.
 */
export function CallPane({ callId, botId, onClose }: { callId: string; botId: string; onClose: () => void }) {
  const bots = useChat((s) => s.bots);
  const nameOf = (id: string) => bots.find((b) => b.id === id)?.name ?? "Bot";
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

  if (!inDesktop) {
    return <section aria-labelledby="side-pane-title" data-slot="call-pane" className="flex h-full min-w-0 flex-col">
      <header className="flex h-10 shrink-0 items-center gap-2 border-b px-3">
        <Button type="button" variant="ghost" size="icon-xs" aria-label="Close call pane" onClick={onClose}><ChevronLeftIcon aria-hidden /></Button>
        <h2 id="side-pane-title" tabIndex={-1} className="truncate text-sm font-medium outline-none">Phone call</h2>
      </header>
      <p className="m-auto p-6 text-center text-sm text-muted-foreground">Call transcripts are available in the desktop app.</p>
    </section>;
  }

  // The header names the bot that started the call; after "put me through" later lines name their own bot.
  const botName = nameOf(call?.botId || botId);
  const multiBot = !!call && messages.some((m) => m.role !== "user" && m.botId !== call.botId);
  const look = call ? callLook(callPart(call)) : null;
  const answering = messages.find((m) => m.role !== "user" && isActive(m.status));
  const liveText = !look?.live ? "" : answering ? `${nameOf(answering.botId)} is answering` : "Call in progress";
  // Cues are the daemon's own prompts and lines without words have nothing to show: neither gets an empty list item.
  const shown = messages.filter((m) => m.role !== "user" || (m.author !== "cue" && !!messageText(m)));
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
          <div className="flex gap-1"><dt>Cost</dt><dd>{call.estCostMicros > 0 ? costText(call.estCostMicros) : "–"}</dd></div>
          <div className="flex gap-1"><dt className="sr-only">Status</dt><dd>{STATUS[call.status] ?? call.status}</dd></div>
        </dl>}
        {call?.reason && <p className="selectable break-words text-xs text-muted-foreground">{call.reason}</p>}
      </div>
    </header>
    {/* Only the live state is announced; the transcript itself is not read out as it grows. */}
    <p role="status" className="sr-only">{liveText}</p>
    <div className="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto p-3">
      {error ? <p role="alert" className="m-auto p-6 text-center text-sm text-muted-foreground">{error}</p>
        : !call ? <p role="status" className="m-auto p-6 text-center text-sm text-muted-foreground">Loading the call…</p>
          : shown.length === 0 ? <p className="m-auto p-6 text-center text-sm text-muted-foreground">{look?.live ? "Nothing has been said yet." : "No transcript for this call."}</p>
            : <ol aria-label="Transcript" className="flex flex-col gap-2">
              {shown.map((message, i) => {
                const speaker = message.role === "user" ? null : message.botId;
                const previous = shown.slice(0, i).findLast((m) => m.role !== "user")?.botId ?? call.botId;
                return <li key={message.id} className="flex min-w-0 flex-col gap-1">
                  {speaker && speaker !== previous && <span data-slot="call-handoff" className="px-1 text-center text-xs text-muted-foreground">Handed to {nameOf(speaker)}</span>}
                  {speaker && multiBot && <span data-slot="call-speaker" className="px-1 text-xs font-medium text-muted-foreground">{nameOf(speaker)}</span>}
                  <Turn message={message} botName={nameOf(message.botId)} bots={bots} />
                </li>;
              })}
            </ol>}
    </div>
  </section>;
}

/** One turn: the owner's line, what reached the bot as input (a report, a code task update), or the bot's line with what it did while answering. */
function Turn({ message, botName, bots }: { message: Message; botName: string; bots: readonly Bot[] }) {
  if (message.role === "user") return <Incoming message={message} bots={bots} />;
  const active = isActive(message.status);
  const rows = message.parts.map((part, i) => <Activity key={i} part={part} active={active} botName={botName} />);
  return <>
    {rows}
    {active && !messageText(message) && <span className="px-1 text-xs text-muted-foreground cv-shimmer">{botName} is answering…</span>}
    {message.status === "error" && <span className="px-1 text-xs text-destructive-foreground">{message.error || "The reply failed."}</span>}
    {message.status === "canceled" && <span className="px-1 text-xs text-muted-foreground">Interrupted</span>}
  </>;
}

/**
 * A user-role message of the thread, drawn by who wrote it: the owner's words as their bubble, a bot's report as the
 * collapsed "reported back" card of the main chat, a code task's update as a collapsed item with a button to the task,
 * anything else as a collapsed item with the author's label. Cues are the daemon's own prompts and never shown.
 */
function Incoming({ message, bots }: { message: Message; bots: readonly Bot[] }) {
  const author = message.author;
  const text = messageText(message);
  if (!text || author === "cue") return null;
  if (!author || author === "user") return <UserBubble><span className="sr-only">You: </span><span className="selectable whitespace-pre-wrap">{text}</span></UserBubble>;
  if (author.startsWith("report:")) return <ChatMessageView message={message} botName="" botAvatar="" bots={bots} className="max-w-full" />;
  if (author.startsWith("task:")) {
    const taskId = author.slice(5);
    return <FollowUp slot="call-task-update" icon={<CodeIcon aria-hidden className="size-4 shrink-0" />} label="Code task update" text={text}
      action={taskId && <Button type="button" variant="ghost" size="xs" data-slot="call-task-open" className="mt-1.5 w-fit text-muted-foreground hover:text-foreground"
        onClick={() => useChat.getState().openTaskPane(taskId)}>Open the task</Button>} />;
  }
  const label = author.startsWith("automation:") ? "From an automation"
    : author.startsWith("bot:") ? `From ${bots.find((b) => b.id === author.slice(4))?.name ?? "another bot"}` : "Message";
  return <FollowUp slot="call-followup" icon={<MessageSquareIcon aria-hidden className="size-4 shrink-0" />} label={label} text={text} />;
}

/**
 * A compact collapsed item for input that reached the bot after the words were spoken; the text opens on click, like the
 * main chat's report card. The text is rendered as markdown (a task's result is markdown) by the same component as bot replies.
 */
function FollowUp({ slot, icon, label, text, action }: { slot: string; icon: ReactNode; label: string; text: string; action?: ReactNode }) {
  return <div data-slot={slot} className="flex w-full min-w-0 flex-col rounded-xl border bg-card/50 px-3 py-2">
    <details className="group">
      <summary className="flex cursor-pointer list-none items-center gap-2 rounded-md text-xs text-muted-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring">
        {icon}
        <span className="min-w-0 flex-1 truncate">{label}</span>
        <span className="group-open:hidden">Show</span><span className="hidden group-open:inline">Hide</span>
      </summary>
      <div className="mt-2 min-w-0 border-s-2 ps-3"><Markdown>{text}</Markdown></div>
    </details>
    {action}
  </div>;
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
