import { SquareTerminal, Workflow } from "lucide-react";
import { AgentHandoff } from "@/components/assistant-ui/elements/agent-handoff";
import { BotAvatar } from "@/components/shell/BotAvatar";
import type { Message } from "@/mygo";
import { cn } from "@/lib/utils";
import type { MentionTarget } from "../mentions";
import { MessageParts, MessageStatusNote, UserParts } from "./MessageParts";
import type { BotRef } from "./WorkTimeline";

export interface ChatMessageViewProps {
  message: Message;
  botName: string;
  botAvatar: string;
  /** Shown on failed/stopped replies when the caller can retry. */
  onRetry?: () => void;
  /** Sends the user's answer to a question the bot asked; absent = questions are read-only. */
  onAnswer?: (answer: string) => void;
  /** Every bot: a user-role message authored by another bot (`bot:<id>`) is drawn as that bot's handoff card, and replies name the bots they handed work to. */
  bots?: readonly BotRef[];
  /** Whether this bot has taken the handoff up (a reply follows it). */
  handoffSettled?: boolean;
  /** Opens a bot's conversation, for the handoffs a reply made. */
  onOpenBot?: (botId: string) => void;
  /** Opens a code task, for the progress reports of tasks this bot started (`task:<id>`). */
  onOpenTask?: (taskId: string) => void;
  /** Rendered under a bot message (the conversation's copy/regenerate row). */
  actions?: React.ReactNode;
  /** Bots and skills a user message may name; they are drawn as chips. */
  mentionTargets?: readonly MentionTarget[];
  className?: string;
}

/** The bubble a user message is drawn in; queued ones (the daemon holds them until the run ends) are dimmed. */
export function UserBubble({ children, queued }: { children: React.ReactNode; queued?: boolean }) {
  return <div data-slot="chat-user" data-queued={queued || undefined} className={cn("flex max-w-[85%] min-w-0 flex-col items-end gap-0.5 self-end", queued && "opacity-60")}>
    <div className="min-w-0 rounded-2xl rounded-br-md bg-secondary px-3 py-1.5 text-sm leading-relaxed text-secondary-foreground">{children}</div>
    {queued && <span className="px-1 text-[0.625rem] font-medium text-muted-foreground">Queued</span>}
  </div>;
}

/**
 * One message exactly as the conversation draws it, without a runtime or any store: used by the bot
 * builder's live preview. User messages are a bubble on the right; bot messages carry the bot's avatar.
 */
export function ChatMessageView({ message, botName, botAvatar, onRetry, onAnswer, bots = [], handoffSettled, onOpenBot, onOpenTask, actions, mentionTargets, className }: ChatMessageViewProps) {
  if (message.role === "user" && message.author.startsWith("report:")) {
    const from = bots.find((b) => b.id === message.author.slice(7));
    const fromName = from?.name ?? "Another bot";
    return <details data-slot="chat-report" className={cn("group w-full max-w-[92%] min-w-0 rounded-xl border bg-card/50 px-3 py-2", className)}>
      <summary className="flex cursor-pointer list-none items-center gap-2 text-xs text-muted-foreground">
        <BotAvatar name={fromName} avatar={from?.avatar ?? ""} className="size-4 text-[9px]" />
        <span className="min-w-0 flex-1 truncate">{fromName} reported back</span>
        <span className="group-open:hidden">Show</span><span className="hidden group-open:inline">Hide</span>
      </summary>
      <div className="selectable mt-2 min-w-0 border-s-2 ps-3"><MessageParts message={message} /></div>
    </details>;
  }
  if (message.role === "user" && message.author.startsWith("bot:")) {
    const from = bots.find((b) => b.id === message.author.slice(4));
    const fromName = from?.name ?? "another bot";
    return <div data-slot="chat-handoff" className={cn("flex w-full min-w-0 flex-col gap-2 rounded-xl border bg-card/50 p-3", className)}>
      <span className="sr-only">Handoff by {fromName}:</span>
      <AgentHandoff from={fromName} to={botName} fromAvatar={<BotAvatar name={fromName} avatar={from?.avatar ?? ""} className="size-4 text-[9px]" />}
        toAvatar={<BotAvatar name={botName} avatar={botAvatar} className="size-4 text-[9px]" />}
        reason={`Handoff by ${fromName}`} carried={[]} settled={handoffSettled ?? false} />
      <div className="selectable min-w-0 border-s-2 ps-3 text-sm leading-relaxed"><UserParts parts={message.parts} /></div>
    </div>;
  }
  if (message.role === "user" && message.author.startsWith("task:")) {
    const taskId = message.author.slice(5);
    return <details data-slot="chat-task-report" className={cn("group w-full max-w-[92%] min-w-0 rounded-xl border bg-card/50 px-3 py-2", className)}>
      <summary className="flex cursor-pointer list-none items-center gap-2 text-xs text-muted-foreground">
        <SquareTerminal aria-hidden className="size-3.5 shrink-0" />
        <span className="min-w-0 flex-1 truncate">Code task update</span>
        {onOpenTask && <button type="button" onClick={(e) => { e.preventDefault(); onOpenTask(taskId); }} className="rounded px-1 font-medium text-foreground outline-none hover:underline focus-visible:ring-2 focus-visible:ring-ring">Open task</button>}
        <span className="group-open:hidden">Show</span><span className="hidden group-open:inline">Hide</span>
      </summary>
      <div className="selectable mt-2 min-w-0 border-s-2 ps-3"><MessageParts message={message} /></div>
    </details>;
  }
  if (message.role === "user" && message.author.startsWith("automation:")) {
    return <div data-slot="chat-automation" className={cn("flex w-full min-w-0 flex-col gap-1.5 rounded-xl border bg-card/50 p-3", className)}>
      <span className="flex items-center gap-1.5 text-xs text-muted-foreground"><Workflow aria-hidden className="size-3.5" />From an automation</span>
      <div className="selectable min-w-0 border-s-2 ps-3 text-sm leading-relaxed"><UserParts parts={message.parts} /></div>
    </div>;
  }
  if (message.role === "user") {
    return <div className={cn("flex w-full flex-col", className)}>
      <span className="sr-only">You said:</span>
      <UserBubble queued={message.status === "pending"}><UserParts parts={message.parts} targets={mentionTargets} /></UserBubble>
    </div>;
  }
  // A reply to an @mention is written by the mentioned bot, not by the bot whose conversation this is.
  const speaker = message.author.startsWith("bot:") ? bots.find((b) => b.id === message.author.slice(4)) : undefined;
  const name = speaker?.name ?? botName;
  const avatar = speaker ? speaker.avatar : botAvatar;
  return <div className={cn("flex w-full min-w-0 gap-2", className)}>
    <BotAvatar name={name} avatar={avatar} className="mt-0.5 size-6 text-xs" />
    <div className="flex min-w-0 flex-1 flex-col gap-1">
      <span className="sr-only">{name} said:</span>
      {speaker && speaker.name !== botName && <span data-slot="chat-speaker" className="text-xs font-medium text-muted-foreground">{name}</span>}
      <MessageParts message={message} onAnswer={onAnswer} botName={name} botAvatar={avatar} bots={bots} onOpenBot={onOpenBot} />
      <MessageStatusNote status={message.status} error={message.error} onRetry={onRetry} />
      {actions}
    </div>
  </div>;
}
