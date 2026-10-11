import { AssistantRuntimeProvider, ThreadPrimitive, useAui, useAuiState } from "@assistant-ui/react";
import { ArrowDownIcon, RotateCcwIcon, XIcon } from "lucide-react";
import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { MessageActions, MessagePair } from "@/components/assistant-ui/elements/message-pair";
import { BotAvatar } from "@/components/shell/BotAvatar";
import { Button } from "@/components/ui/button";
import type { Bot, Message } from "@/mygo";
import { useChat } from "@/stores/chat";
import { selectConversation, useBotRunning, useChatMessages } from "@/stores/chatMessages";
import { Composer } from "./Composer";
import { ChatMessageView } from "./parts/MessageBubble";
import { hasVisibleParts } from "./parts/MessageParts";
import { messageText } from "./parts/format";
import { CopyButton } from "./parts/Sources";
import { SOURCE_KEY } from "./runtime/convert";
import { useChatRuntime } from "./runtime/useChatRuntime";
import { ThreadSearch } from "./ThreadSearch";
import { targetsFor, useBotInfos, type BotInfo, type MentionTarget } from "./mentions";

/** How close to the top (px) the reader gets before the next older page is fetched. */
const LOAD_OLDER_AT = 160;

function isDaemonMessage(value: unknown): value is Message {
  return typeof value === "object" && value !== null && "id" in value && "parts" in value && "role" in value;
}

/** One message: the shared renderer, the hover actions, and Retry for a failed last reply. */
const Turn = memo(function Turn({ bot, bots, targets, running }: { bot: Bot; bots: readonly BotInfo[]; targets: readonly MentionTarget[]; running: boolean }) {
  const raw: unknown = useAuiState((s) => s.message.metadata.custom?.[SOURCE_KEY]);
  const isLast = useAuiState((s) => s.message.isLast);
  if (!isDaemonMessage(raw) || (raw.role === "user" && !hasVisibleParts(raw.parts))) return null;
  const assistant = raw.role !== "user";
  const settled = raw.status !== "running" && raw.status !== "pending";
  const canRegenerate = assistant && isLast && !running;
  return <MessagePair>
    <ChatMessageView message={raw} botName={bot.name} botAvatar={bot.avatar}
      bots={bots} handoffSettled={!isLast} mentionTargets={targets}
      onOpenBot={(botId) => useChat.getState().open(botId)}
      onRetry={canRegenerate ? () => void useChatMessages.getState().regenerate(bot.id, raw.id) : undefined}
      onAnswer={canRegenerate && raw.status === "complete" ? (text) => void useChatMessages.getState().send(bot.id, { text }) : undefined}
      actions={assistant && settled && <MessageActions regenerate={canRegenerate}><CopyButton text={messageText(raw)} label="Copy response" /></MessageActions>} />
  </MessagePair>;
});

function EmptyConversation({ bot }: { bot: Bot }) {
  return <div data-slot="conversation-empty" className="mx-auto my-auto flex max-w-md flex-col items-center gap-2 py-12 text-center">
    <BotAvatar name={bot.name} avatar={bot.avatar} className="size-14 text-xl" />
    <h3 className="text-base font-semibold">{bot.name}</h3>
    {bot.description && <p className="selectable text-sm text-muted-foreground">{bot.description}</p>}
    <p className="text-sm text-muted-foreground">Say hello to start the conversation.</p>
  </div>;
}

/** Announces when a reply starts and finishes; streaming text itself is not read out token by token. */
function LiveStatus({ running }: { running: boolean }) {
  const [text, setText] = useState("");
  const was = useRef(running);
  useEffect(() => {
    if (running !== was.current) setText(running ? "Responding" : "Done");
    was.current = running;
  }, [running]);
  return <div role="status" aria-live="polite" className="sr-only">{text}</div>;
}

function Thread({ bot, running }: { bot: Bot; running: boolean }) {
  const aui = useAui();
  const conversation = useChatMessages(selectConversation(bot.id));
  // Read once for the whole thread: each turn gets them as props, so a streaming reply never makes every turn re-subscribe.
  const bots = useBotInfos();
  const targets = useMemo(() => targetsFor(bots, bot.id), [bots, bot.id]);
  const renderTurn = useCallback(() => <Turn bot={bot} bots={bots} targets={targets} running={running} />, [bot, bots, targets, running]);
  const problem = useChatMessages((s) => s.problems[bot.id]);
  const viewport = useRef<HTMLDivElement>(null);
  const thread = useRef<HTMLDivElement>(null);
  const [searching, setSearching] = useState(false);
  const before = useRef<{ height: number; top: number } | null>(null);
  const firstId = conversation.messages[0]?.id;

  useEffect(() => useChatMessages.getState().startSync(), []);
  // Ctrl/Cmd+F finds inside this conversation instead of the page; a handler that already took the key (an editor) wins.
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.altKey || event.shiftKey || !(event.ctrlKey || event.metaKey) || event.key.toLowerCase() !== "f") return;
      event.preventDefault();
      setSearching(true);
      document.querySelector<HTMLInputElement>("[data-slot=thread-search] input")?.select();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);
  const closeSearch = () => {
    setSearching(false);
    document.querySelector<HTMLElement>("[data-slot=composer] textarea")?.focus();
  };
  useEffect(() => { void useChatMessages.getState().load(bot.id); }, [bot.id]);

  // Prepending older messages must not move what the reader is looking at.
  useLayoutEffect(() => {
    const el = viewport.current;
    const anchor = before.current;
    if (!el || !anchor) return;
    before.current = null;
    el.scrollTop = anchor.top + (el.scrollHeight - anchor.height);
  }, [firstId]);

  const loadOlder = () => {
    const el = viewport.current;
    if (!el || !conversation.hasMore || conversation.loadingOlder) return;
    before.current = { height: el.scrollHeight, top: el.scrollTop };
    void aui.thread.loadEarlier();
  };

  const showEmpty = conversation.loaded && conversation.messages.length === 0;
  return <>
    <ThreadPrimitive.Root ref={thread} className="relative flex min-h-0 flex-1 flex-col">
      {searching && <ThreadSearch messages={conversation.messages} scope={thread} onClose={closeSearch} />}
      <ThreadPrimitive.Viewport ref={viewport} role="region" aria-label={`Conversation with ${bot.name}`} tabIndex={0}
        onScroll={(event) => { if (event.currentTarget.scrollTop < LOAD_OLDER_AT) loadOlder(); }}
        className="flex min-h-0 flex-1 flex-col overflow-y-auto outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring">
        <div className="mx-auto flex min-h-full w-full max-w-3xl shrink-0 flex-col gap-5 px-4 pt-6 pb-12">
          {!conversation.loaded && !conversation.error && <p role="status" className="m-auto text-sm text-muted-foreground">Loading conversation…</p>}
          {!conversation.loaded && conversation.error && <div role="alert" className="m-auto flex flex-col items-center gap-2 text-center text-sm">
            <p className="selectable text-destructive-foreground">Could not load this conversation: {conversation.error}</p>
            <Button type="button" variant="outline" size="sm" onClick={() => void useChatMessages.getState().load(bot.id)}><RotateCcwIcon aria-hidden />Retry</Button>
          </div>}
          {conversation.hasMore && <Button type="button" variant="ghost" size="sm" className="self-center text-muted-foreground" disabled={conversation.loadingOlder} onClick={loadOlder}>
            {conversation.loadingOlder ? "Loading earlier messages…" : "Load earlier messages"}
          </Button>}
          {showEmpty && <EmptyConversation bot={bot} />}
          {conversation.loaded && !conversation.hasMore && conversation.messages[0] && (
            <time dateTime={new Date(conversation.messages[0].createdAt).toISOString()} className="self-center text-muted-foreground text-xs">{startedAt(conversation.messages[0].createdAt)}</time>
          )}
          <ThreadPrimitive.Messages>{renderTurn}</ThreadPrimitive.Messages>
        </div>
      </ThreadPrimitive.Viewport>
      <ThreadPrimitive.ScrollToBottom asChild>
        <Button type="button" variant="outline" size="icon-sm" aria-label="Jump to latest" className="absolute bottom-3 left-1/2 -translate-x-1/2 rounded-full bg-background disabled:hidden"><ArrowDownIcon aria-hidden /></Button>
      </ThreadPrimitive.ScrollToBottom>
    </ThreadPrimitive.Root>
    {problem && <div role="alert" className="mx-auto flex w-full max-w-3xl items-start gap-2 px-4 pb-2 text-xs">
      <p className="selectable min-w-0 flex-1 rounded-lg border border-destructive/30 bg-destructive/8 px-2.5 py-1.5 text-destructive-foreground">{problem.message}</p>
      {problem.request && <Button type="button" variant="outline" size="xs" onClick={() => void useChatMessages.getState().retrySend(bot.id)}><RotateCcwIcon aria-hidden />Retry</Button>}
      <Button type="button" variant="ghost" size="icon-xs" aria-label="Dismiss" onClick={() => useChatMessages.getState().dismissProblem(bot.id)}><XIcon aria-hidden /></Button>
    </div>}
    <LiveStatus running={running} />
    <Composer bot={bot} running={running} />
  </>;
}

/** One bot's conversation: scrolling thread backed by the daemon, composer. Mount it keyed by bot id. */
export function Conversation({ bot }: { bot: Bot }) {
  const running = useBotRunning(bot.id);
  const runtime = useChatRuntime(bot, running);
  return <AssistantRuntimeProvider runtime={runtime}>
    <div data-slot="conversation" className="flex h-full min-h-0 flex-col">
      <Thread bot={bot} running={running} />
    </div>
  </AssistantRuntimeProvider>;
}

/** "Today 11:44 AM", "Yesterday 9:02 PM" or "Oct 3, 9:02 PM": when the conversation began. */
function startedAt(ms: number): string {
  const at = new Date(ms);
  const time = at.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
  const days = Math.round((new Date().setHours(0, 0, 0, 0) - new Date(ms).setHours(0, 0, 0, 0)) / 86_400_000);
  if (days === 0) return `Today ${time}`;
  if (days === 1) return `Yesterday ${time}`;
  return `${at.toLocaleDateString([], { month: "short", day: "numeric", year: at.getFullYear() === new Date().getFullYear() ? undefined : "numeric" })}, ${time}`;
}
