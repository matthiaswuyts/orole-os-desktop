import { BotAvatar } from "@/components/shell/BotAvatar";
import type { Message } from "@/mygo";
import { cn } from "@/lib/utils";
import { MessageParts, MessageStatusNote, UserParts } from "./MessageParts";

export interface ChatMessageViewProps {
  message: Message;
  botName: string;
  botAvatar: string;
  /** Shown on failed/stopped replies when the caller can retry. */
  onRetry?: () => void;
  /** Sends the user's answer to a question the bot asked; absent = questions are read-only. */
  onAnswer?: (answer: string) => void;
  /** Rendered under a bot message (the conversation's copy/regenerate row). */
  actions?: React.ReactNode;
  className?: string;
}

/** The bubble a user message is drawn in; queued ones (the daemon holds them until the run ends) are dimmed. */
export function UserBubble({ children, queued }: { children: React.ReactNode; queued?: boolean }) {
  return <div data-slot="chat-user" data-queued={queued || undefined} className={cn("flex max-w-[85%] min-w-0 flex-col items-end gap-0.5 self-end", queued && "opacity-60")}>
    <div className="min-w-0 rounded-2xl bg-secondary px-3 py-1.5 text-sm leading-relaxed text-secondary-foreground">{children}</div>
    {queued && <span className="px-1 text-[0.625rem] font-medium text-muted-foreground">Queued</span>}
  </div>;
}

/**
 * One message exactly as the conversation draws it, without a runtime or any store: used by the bot
 * builder's live preview. User messages are a bubble on the right; bot messages carry the bot's avatar.
 */
export function ChatMessageView({ message, botName, botAvatar, onRetry, onAnswer, actions, className }: ChatMessageViewProps) {
  if (message.role === "user") {
    return <div className={cn("flex w-full flex-col", className)}>
      <span className="sr-only">You said:</span>
      <UserBubble queued={message.status === "pending"}><UserParts parts={message.parts} /></UserBubble>
    </div>;
  }
  return <div className={cn("flex w-full min-w-0 gap-2", className)}>
    <BotAvatar name={botName} avatar={botAvatar} className="mt-0.5 size-6 text-xs" />
    <div className="flex min-w-0 flex-1 flex-col gap-1.5">
      <span className="sr-only">{botName} said:</span>
      <MessageParts message={message} onAnswer={onAnswer} />
      <MessageStatusNote status={message.status} error={message.error} onRetry={onRetry} />
      {actions}
    </div>
  </div>;
}
