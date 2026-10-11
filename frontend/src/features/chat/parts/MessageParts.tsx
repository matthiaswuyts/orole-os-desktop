import { CircleAlertIcon, CircleSlashIcon, RotateCcwIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Markdown } from "@/components/Markdown";
import { Button } from "@/components/ui/button";
import type { Message, MsgPart } from "@/mygo";
import { isActive, sourcesOf } from "./format";
import { NoticeLine } from "./Steps";
import { WorkTimeline, type BotRef } from "./WorkTimeline";
import { workOf } from "./work";
import { MemoryChips } from "@/components/assistant-ui/elements/memory-chips";
import { AttachmentPill, ReportCard, SourcesButton } from "./Sources";
import { QuestionCard } from "./Question";
import { StructuredPart } from "./StructuredPart";
import { MentionText } from "../MentionText";
import type { MentionTarget } from "../mentions";
import { splitQuote } from "../quote";
import { splitBubbles } from "./bubbles";
import { cn } from "@/lib/utils";
import { WorkingStatus } from "@/components/assistant-ui/elements/working-status";

/** Whether a user message has anything to draw (the setup kickoff is text the user never sees). */
export const hasVisibleParts = (parts: readonly MsgPart[]): boolean => parts.some((p) => p.type === "attachment" || (p.type === "text" && !p.hidden));

/** The sent text and files of a user message; plain text, never markdown. */
export function UserParts({ parts, targets }: { parts: readonly MsgPart[]; /** bots and skills to draw as chips; absent = plain text */ targets?: readonly MentionTarget[] }) {
  const files = parts.filter((p) => p.type === "attachment");
  const text = parts.filter((p) => p.type === "text" && !p.hidden).map((p) => p.text ?? "").join("\n\n");
  const { quote, rest } = splitQuote(text);
  return <>
    {files.length > 0 && <div className="mb-1 flex flex-wrap gap-1.5">{files.map((p, i) => <AttachmentPill key={p.attachmentId || i} name={p.name || p.title} mime={p.mime} size={p.size} />)}</div>}
    {quote && <blockquote data-slot="chat-quote" className="selectable mb-1.5 border-s-2 border-primary/60 ps-2.5 text-muted-foreground text-xs whitespace-pre-wrap break-words">{quote}</blockquote>}
    {rest && <p className="selectable whitespace-pre-wrap break-words">{targets?.length ? <MentionText text={rest} targets={targets} /> : rest}</p>}
  </>;
}

/**
 * Renders what an assistant message holds: the work it did behind one collapsed timeline (steps, web search, research
 * outline), then the answer in order (text, report, questions, notices), then the memory it wrote as chips. Sources live in the
 * timeline and, for a report, in its "N sources" popover; they are never listed in the answer.
 * Pure over its props, so the builder preview can use it without a runtime.
 */
export function MessageParts({ message, onAnswer, botName = "This bot", botAvatar = "", bots = [], onOpenBot }: { message: Pick<Message, "parts" | "status">; /** Sends the user's answer to a question; absent = questions are read-only. */ onAnswer?: (answer: string) => void; /** Who wrote the message, named in its handoffs. */ botName?: string; botAvatar?: string; /** Every bot, to draw the avatars of the bots a handoff names. */ bots?: readonly BotRef[]; /** Opens another bot's conversation (a handoff's worker); absent = no link. */ onOpenBot?: (botId: string) => void }) {
  const active = isActive(message.status);
  const hasReport = message.parts.some((p) => p.type === "report");
  const sources = sourcesOf(message.parts);
  const work = workOf(message.parts, active);
  const memory = message.parts.filter((p) => p.type === "memory");
  const blocks: ReactNode[] = [];
  if (work.count > 0) blocks.push(<WorkTimeline key="work" work={work} sources={sources} botName={botName} botAvatar={botAvatar} bots={bots} onOpenBot={onOpenBot} />);
  message.parts.forEach((part, i) => {
    switch (part.type) {
      case "text":
        if (part.text) blocks.push(<Markdown key={i}>{part.text}</Markdown>);
        break;
      case "report":
        blocks.push(<ReportCard key={i} title={part.title} text={part.text ?? ""} sources={sources} />);
        break;
      case "question":
        if (part.text && part.choices?.length) blocks.push(<QuestionCard key={i} text={part.text} choices={part.choices} onAnswer={onAnswer} />);
        break;
      case "ui":
        blocks.push(<StructuredPart key={i} part={part} />);
        break;
      case "notice":
        if (part.text) blocks.push(<NoticeLine key={i} text={part.text} />);
        break;
      default:
        break; // work rows go to the timeline, memory writes to the chips below; citations ride along; attachments belong to user messages
    }
  });
  if (memory.length) blocks.push(<MemoryChips key="memory" chips={memory.map((part, i) => ({ id: part.id || `m${i}`, text: part.text || "Memory updated", change: "added" as const }))} />);
  if (!hasReport && sources.length && work.count === 0) blocks.push(<SourcesButton key="sources" sources={sources} />);
  if (!blocks.length && active) return <TypingIndicator />;
  return <>{blocks}</>;
}

/** Why a reply stopped early, with Retry where the caller can honour it. Nothing is shown for a healthy message. */
export function MessageStatusNote({ status, error, onRetry }: { status?: string; error?: string; onRetry?: () => void }) {
  if (status === "error") {
    return <div role="alert" data-slot="chat-error" className="flex min-w-0 items-start gap-2 rounded-lg border border-destructive/30 bg-destructive/8 px-2.5 py-1.5 text-xs text-destructive-foreground">
      <CircleAlertIcon aria-hidden className="mt-0.5 size-3.5 shrink-0 text-destructive" />
      <span className="selectable min-w-0 flex-1 break-words">{error || "The reply failed."}</span>
      {onRetry && <Button type="button" variant="outline" size="xs" onClick={onRetry}><RotateCcwIcon aria-hidden />Retry</Button>}
    </div>;
  }
  if (status === "canceled" || status === "incomplete") {
    return <p data-slot="chat-stopped" className="flex items-center gap-1.5 text-xs text-muted-foreground">
      <CircleSlashIcon aria-hidden className="size-3.5 shrink-0" />
      {status === "canceled" ? "Stopped" : "This reply was interrupted"}
      {onRetry && <Button type="button" variant="ghost" size="xs" onClick={onRetry}><RotateCcwIcon aria-hidden />Retry</Button>}
    </p>;
  }
  return null;
}
