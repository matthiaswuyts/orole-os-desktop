import { useEffect, useRef, useState } from "react";
import { RotateCcw, SendHorizontal, Square } from "lucide-react";
import { Chat, events, type DraftTurn, type Message } from "@/mygo";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { errText } from "@/features/projects/dialogs";
import { cn } from "@/lib/utils";
import { useHasProvider, useLLM } from "@/stores/llm";
import { useUI } from "@/stores/ui";
import { ChatMessageView, TypingIndicator, messageText } from "../parts";
import { toInput, type FormState, type Problems } from "./botForm";

/** Prior turns the daemon accepts for one preview. */
const MAX_TURNS = 100;
const DRAFT_PREFIX = "draft:";

const newDraftId = () => crypto.randomUUID();

/** Replaces by id (a streaming reply updates in place) or appends. */
function upsert(list: readonly Message[], message: Message): Message[] {
  const at = list.findIndex((m) => m.id === message.id);
  if (at < 0) return [...list, message];
  const next = list.slice();
  next[at] = message;
  return next;
}

/** Completed turns, so a failed or stopped reply is not fed back as context. */
function historyOf(messages: readonly Message[]): DraftTurn[] {
  return messages
    .filter((m) => m.status === "complete" && (m.role === "user" || m.role === "assistant"))
    .map((m) => ({ role: m.role, text: messageText(m) }))
    .filter((t) => t.text.trim() !== "")
    .slice(-MAX_TURNS);
}

/** Why the preview cannot run right now, or null. */
function blocker(hasProvider: boolean | null, llmError: string | null, problems: Problems): string | null {
  if (hasProvider === null) return llmError ? `Could not check your providers: ${llmError}` : "Checking which providers are connected…";
  if (!hasProvider) return "No provider is connected. Connect one to try this bot.";
  const count = Object.keys(problems).length;
  if (count > 0) return `Fix ${count === 1 ? "the problem" : `${count} problems`} in the form to preview the bot.`;
  return null;
}

/** Runs the unsaved form against the daemon. Nothing is stored; the exchange lives only in this component. */
export function PreviewPane({ form, problems, className }: { form: FormState; problems: Problems; className?: string }) {
  const hasProvider = useHasProvider();
  const llmError = useLLM((s) => s.error);
  const [draftId, setDraftId] = useState(newDraftId);
  const [messages, setMessages] = useState<Message[]>([]);
  const [running, setRunning] = useState(false);
  const [sending, setSending] = useState(false);
  const [text, setText] = useState("");
  const [error, setError] = useState<string | null>(null);
  const scroller = useRef<HTMLDivElement>(null);
  const live = useRef({ draftId, busy: false });

  const busy = sending || running || messages.some((m) => m.status === "running" || m.status === "pending");
  useEffect(() => { live.current = { draftId, busy }; });

  useEffect(() => {
    const key = `${DRAFT_PREFIX}${draftId}`;
    const offMessage = events.chatMessage.on((message) => { if (message.botId === key) setMessages((all) => upsert(all, message)); });
    const offRun = events.chatRun.on(({ botId, running: now }) => { if (botId === key) setRunning(now); });
    return () => { offMessage(); offRun(); };
  }, [draftId]);

  // A reply that is still streaming when the builder closes would otherwise keep spending tokens.
  useEffect(() => () => { if (live.current.busy) void Chat.cancel(`${DRAFT_PREFIX}${live.current.draftId}`).catch(() => undefined); }, []);

  useEffect(() => {
    const el = scroller.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messages]);

  const blocked = blocker(hasProvider, llmError, problems);

  async function send() {
    const message = text.trim();
    if (!message || busy || blocked) return;
    setError(null);
    setSending(true);
    try {
      const stored = await Chat.draftSend({ draftId, bot: toInput(form, ""), history: historyOf(messages), text: message });
      setMessages((all) => upsert(all, stored));
      setText("");
    } catch (cause) {
      setError(errText(cause));
    } finally {
      setSending(false);
    }
  }

  function reset() {
    if (busy) void Chat.cancel(`${DRAFT_PREFIX}${draftId}`).catch(() => undefined);
    // A new id: late events of the old run no longer match and are ignored.
    setDraftId(newDraftId());
    setMessages([]);
    setRunning(false);
    setError(null);
  }

  const name = form.name.trim() || "New bot";
  const last = messages[messages.length - 1];
  const waiting = busy && (!last || last.role === "user" || (last.parts?.length ?? 0) === 0);

  return (
    <section aria-label="Preview" className={cn("flex min-h-0 min-w-0 flex-col rounded-lg border bg-card", className)}>
      <header className="flex items-center justify-between gap-2 border-b px-3 py-2">
        <div className="flex min-w-0 flex-col">
          <h2 className="font-medium text-sm">Preview</h2>
          <p className="text-muted-foreground text-xs">Preview - not saved, memory disabled</p>
        </div>
        <Button size="xs" variant="ghost" onClick={reset} disabled={messages.length === 0 && !busy && !error}><RotateCcw />Reset preview</Button>
      </header>
      <div ref={scroller} role="log" aria-label="Preview conversation" aria-live="polite" className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-3">
        {messages.length === 0 && !error && (
          <p className="m-auto max-w-64 text-center text-muted-foreground text-xs">Say something to see how the bot answers with the settings on the left. It calls the real model and tools, but remembers nothing and cannot ask other bots.</p>
        )}
        {messages.map((m) => <ChatMessageView key={m.id} message={m} botName={name} botAvatar={form.avatar} botColor={form.color} />)}
        {waiting && <TypingIndicator />}
      </div>
      <div className="flex flex-col gap-1.5 border-t p-2">
        {blocked && (
          <p role="status" className="text-muted-foreground text-xs">
            {blocked}
            {hasProvider === false && <Button size="xs" variant="link" className="ms-1" onClick={() => useUI.getState().openSettings("ai")}>Connect a provider</Button>}
          </p>
        )}
        {error && <p role="alert" className="selectable break-words text-destructive-foreground text-xs">{error}</p>}
        <div className="flex items-end gap-1.5">
          <Textarea
            size="sm" className="flex-1" aria-label="Preview message" placeholder={blocked ? "Preview unavailable" : "Message the preview"} value={text} disabled={blocked !== null}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); void send(); } }}
          />
          {busy
            ? <Button size="icon" variant="outline" aria-label="Stop the preview reply" onClick={() => void Chat.cancel(`${DRAFT_PREFIX}${draftId}`).catch((cause: unknown) => setError(errText(cause)))}><Square /></Button>
            : <Button size="icon" aria-label="Send preview message" disabled={blocked !== null || !text.trim()} onClick={() => void send()}><SendHorizontal /></Button>}
        </div>
      </div>
    </section>
  );
}
