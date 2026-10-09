import { CircleAlertIcon, CircleSlashIcon, RotateCcwIcon } from "lucide-react";
import { Fragment, type ReactNode } from "react";
import { Markdown } from "@/components/Markdown";
import { Button } from "@/components/ui/button";
import type { Message, MsgPart } from "@/mygo";
import { isActive, sourcesOf } from "./format";
import { DelegationRow, MemoryChip, NoticeLine, ReasoningBlock, StepList, ToolRow } from "./Steps";
import { AttachmentPill, ReportCard, SourcesButton } from "./Sources";

const STEP_TYPES = new Set(["tool", "delegation"]);

/** Three quiet dots while a reply has produced nothing yet. */
export function TypingIndicator() {
  return <span aria-hidden data-slot="chat-typing" className="inline-flex items-center gap-1 py-1.5">
    {[0, 1, 2].map((i) => <span key={i} className="size-1.5 animate-pulse rounded-full bg-muted-foreground/60 motion-reduce:animate-none" style={{ animationDelay: `${i * 160}ms` }} />)}
  </span>;
}

/** The sent text and files of a user message; plain text, never markdown. */
export function UserParts({ parts }: { parts: readonly MsgPart[] }) {
  const files = parts.filter((p) => p.type === "attachment");
  const text = parts.filter((p) => p.type === "text").map((p) => p.text ?? "").join("\n\n");
  return <>
    {files.length > 0 && <div className="mb-1 flex flex-wrap gap-1.5">{files.map((p, i) => <AttachmentPill key={p.attachmentId || i} name={p.name || p.title} mime={p.mime} size={p.size} />)}</div>}
    {text && <p className="selectable whitespace-pre-wrap break-words">{text}</p>}
  </>;
}

/**
 * Renders what an assistant message holds, in order: reasoning, activity rows, text, report, memory
 * writes, notices; citations are gathered into one "N sources" popover at the end (or into the report).
 * Pure over its props, so the builder preview can use it without a runtime.
 */
export function MessageParts({ message }: { message: Pick<Message, "parts" | "status"> }) {
  const active = isActive(message.status);
  const hasReport = message.parts.some((p) => p.type === "report");
  const sources = sourcesOf(message.parts);
  const blocks: ReactNode[] = [];
  const parts = message.parts;
  for (let i = 0; i < parts.length; i++) {
    const part = parts[i]!;
    const last = i === parts.length - 1;
    if (STEP_TYPES.has(part.type)) {
      const start = i;
      while (i + 1 < parts.length && STEP_TYPES.has(parts[i + 1]!.type)) i++;
      blocks.push(<StepList key={`steps-${start}`}>{parts.slice(start, i + 1).map((step, k) => <Fragment key={step.id || k}>
        {step.type === "tool"
          ? <ToolRow name={step.name} status={step.status} summary={step.summary} durationMs={step.durationMs} active={active} />
          : <DelegationRow name={step.name} status={step.status} summary={step.summary} durationMs={step.durationMs} active={active} />}
      </Fragment>)}</StepList>);
      continue;
    }
    switch (part.type) {
      case "reasoning":
        if (part.text) blocks.push(<ReasoningBlock key={i} text={part.text} running={active && last} durationMs={part.durationMs} />);
        break;
      case "text":
        if (part.text) blocks.push(<Markdown key={i}>{part.text}</Markdown>);
        break;
      case "report":
        blocks.push(<ReportCard key={i} title={part.title} text={part.text ?? ""} sources={sources} />);
        break;
      case "memory":
        blocks.push(<MemoryChip key={i} text={part.text} />);
        break;
      case "notice":
        if (part.text) blocks.push(<NoticeLine key={i} text={part.text} />);
        break;
      default:
        break; // citations are gathered below; attachments belong to user messages
    }
  }
  if (!hasReport && sources.length) blocks.push(<SourcesButton key="sources" sources={sources} />);
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
