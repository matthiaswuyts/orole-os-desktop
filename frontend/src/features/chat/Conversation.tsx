import { AssistantRuntimeProvider, ThreadPrimitive, useAui, useAuiState } from "@assistant-ui/react";
import { ArrowDownIcon, RotateCcwIcon, XIcon } from "lucide-react";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { MessageActions, MessagePair } from "@/components/assistant-ui/elements/message-pair";
import { BotAvatar } from "@/components/shell/BotAvatar";
import { Button } from "@/components/ui/button";
import type { Bot, Message } from "@/mygo";
import { selectConversation, useBotRunning, useChatMessages } from "@/stores/chatMessages";
import { Composer } from "./Composer";
import { ConversationHeader } from "./ConversationHeader";
import { ChatMessageView } from "./parts/MessageBubble";
import { messageText } from "./parts/format";
import { CopyButton } from "./parts/Sources";
import { SOURCE_KEY } from "./runtime/convert";
import { useChatRuntime } from "./runtime/useChatRuntime";

/** How close to the top (px) the reader gets before the next older page is fetched. */
const LOAD_OLDER_AT = 160;

function isDaemonMessage(value: unknown): value is Message {
  return typeof value === "object" && value !== null && "id" in value && "parts" in value && "role" in value;
}

/** One message: the shared renderer, the hover actions, and Retry for a failed last reply. */
function Turn({ bot }: { bot: Bot }) {
  const raw: unknown = useAuiState((s) => s.message.metadata.custom?.[SOURCE_KEY]);
  const isLast = useAuiState((s) => s.message.isLast);
  const running = useBotRunning(bot.id);
  if (!isDaemonMessage(raw)) return null;
  const assistant = raw.role !== "user";
  const settled = raw.status !== "running" && raw.status !== "pending";
  const canRegenerate = assistant && isLast && !running;
  return <MessagePair>
    <ChatMessageView message={raw} botName={bot.name} botAvatar={bot.avatar} botColor={bot.color}
      onRetry={canRegenerate ? () => void useChatMessages.getState().regenerate(bot.id, raw.id) : undefined}
      actions={assistant && settled && <MessageActions regenerate={canRegenerate}><CopyButton text={messageText(raw)} label="Copy response" /></MessageActions>} />
  </MessagePair>;
}

function EmptyConversation({ bot }: { bot: Bot }) {
  return <div data-slot="conversation-empty" className="mx-auto my-auto flex max-w-md flex-col items-center gap-2 py-12 text-center">
    <BotAvatar name={bot.name} avatar={bot.avatar} color={bot.color} className="size-14 text-xl" />
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

function Thread({ bot }: { bot: Bot }) {
  const aui = useAui();
  const conversation = useChatMessages(selectConversation(bot.id));
  const running = useBotRunning(bot.id);
  const problem = useChatMessages((s) => s.problems[bot.id]);
  const viewport = useRef<HTMLDivElement>(null);
  const before = useRef<{ height: number; top: number } | null>(null);
  const firstId = conversation.messages[0]?.id;

  useEffect(() => useChatMessages.getState().startSync(), []);
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
    <ThreadPrimitive.Root className="relative flex min-h-0 flex-1 flex-col">
      <ThreadPrimitive.Viewport ref={viewport} role="region" aria-label={`Conversation with ${bot.name}`} tabIndex={0}
        onScroll={(event) => { if (event.currentTarget.scrollTop < LOAD_OLDER_AT) loadOlder(); }}
        className="flex min-h-0 flex-1 flex-col overflow-y-auto outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring">
        <div className="mx-auto flex min-h-full w-full max-w-3xl flex-col gap-5 px-4 py-6">
          {!conversation.loaded && !conversation.error && <p role="status" className="m-auto text-sm text-muted-foreground">Loading conversation…</p>}
          {!conversation.loaded && conversation.error && <div role="alert" className="m-auto flex flex-col items-center gap-2 text-center text-sm">
            <p className="selectable text-destructive-foreground">Could not load this conversation: {conversation.error}</p>
            <Button type="button" variant="outline" size="sm" onClick={() => void useChatMessages.getState().load(bot.id)}><RotateCcwIcon aria-hidden />Retry</Button>
          </div>}
          {conversation.hasMore && <Button type="button" variant="ghost" size="sm" className="self-center text-muted-foreground" disabled={conversation.loadingOlder} onClick={loadOlder}>
            {conversation.loadingOlder ? "Loading earlier messages…" : "Load earlier messages"}
          </Button>}
          {showEmpty && <EmptyConversation bot={bot} />}
          <ThreadPrimitive.Messages>{() => <Turn bot={bot} />}</ThreadPrimitive.Messages>
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
    <Composer bot={bot} />
  </>;
}

/** One bot's conversation: header, scrolling thread backed by the daemon, composer. Mount it keyed by bot id. */
export function Conversation({ bot }: { bot: Bot }) {
  const runtime = useChatRuntime(bot);
  return <AssistantRuntimeProvider runtime={runtime}>
    <div data-slot="conversation" className="flex h-full min-h-0 flex-col">
      <ConversationHeader bot={bot} />
      <Thread bot={bot} />
    </div>
  </AssistantRuntimeProvider>;
}
