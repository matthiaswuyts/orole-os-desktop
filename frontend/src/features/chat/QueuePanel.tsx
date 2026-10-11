import { CornerDownRightIcon, PauseIcon, PlayIcon, XIcon } from "lucide-react";
import { memo } from "react";
import { useShallow } from "zustand/react/shallow";
import { Button } from "@/components/ui/button";
import type { Bot, Message } from "@/mygo";
import { selectConversation, useChatMessages } from "@/stores/chatMessages";
import { messageText } from "./parts/format";

/** The user's messages the daemon is holding behind the active reply, oldest first. */
export const waitingMessages = (messages: readonly Message[]): Message[] => messages.filter((m) => m.role === "user" && m.author === "user" && m.status === "pending");

/** What a waiting message says, on one line; a message with only files says so. */
function preview(message: Message): string {
  const text = messageText(message).replace(/\s+/g, " ").trim();
  if (text) return text;
  const files = message.parts.filter((p) => p.type === "attachment").length;
  return files ? `${files} file${files === 1 ? "" : "s"}` : "Message";
}

/**
 * The queue above the composer: messages sent while the bot is busy wait here, in order. Each can be canceled on
 * its own, and the whole queue can be paused (the bot finishes what it is doing, then holds) and resumed.
 * Pausing is the daemon's; this only shows its state and sends the request.
 */
export const QueuePanel = memo(function QueuePanel({ bot, running }: { bot: Bot; running: boolean }) {
  // Shallow: the waiting messages themselves do not change while the reply streams, so neither does this panel.
  const waiting = useChatMessages(useShallow((s) => waitingMessages(selectConversation(bot.id)(s).messages)));
  const paused = useChatMessages((s) => s.paused[bot.id] ?? false);
  if (waiting.length === 0) return null;
  const title = paused ? `Paused · ${waiting.length} waiting` : running ? `Waiting · ${bot.name} is working` : `Starting next · ${waiting.length} waiting`;
  return <section data-slot="queue-panel" aria-label="Queued messages" className="mx-auto -mb-3 flex w-full max-w-3xl shrink-0 flex-col px-4">
    <div className="flex flex-col gap-0.5 rounded-t-2xl border border-b-0 bg-card/60 px-3 pt-2 pb-4">
      <div className="flex items-center gap-2">
        <h2 className="min-w-0 flex-1 truncate text-muted-foreground text-xs">{title}</h2>
        {paused
          ? <Button type="button" variant="ghost" size="xs" onClick={() => void useChatMessages.getState().resumeQueue(bot.id)}><PlayIcon aria-hidden />Resume</Button>
          : <Button type="button" variant="ghost" size="xs" onClick={() => void useChatMessages.getState().pauseQueue(bot.id)}><PauseIcon aria-hidden />Pause</Button>}
      </div>
      <ol className="flex flex-col">
        {waiting.map((message) => <li key={message.id} className="group/queued flex items-center gap-2 py-0.5 text-sm">
          <CornerDownRightIcon aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
          <span className="selectable min-w-0 flex-1 truncate">{preview(message)}</span>
          <Button type="button" variant="ghost" size="icon-xs" aria-label={`Cancel queued message: ${preview(message)}`} onClick={() => void useChatMessages.getState().cancelQueued(bot.id, message.id)}><XIcon aria-hidden /></Button>
        </li>)}
      </ol>
    </div>
  </section>;
});
