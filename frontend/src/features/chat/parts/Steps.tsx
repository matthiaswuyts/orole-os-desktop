import { InfoIcon } from "lucide-react";

/** Engine notices (compaction, budget stops): informational, not errors. */
export function NoticeLine({ text }: { text: string }) {
  return <p role="note" data-slot="chat-notice" className="flex items-start gap-1.5 text-xs text-muted-foreground">
    <InfoIcon aria-hidden className="mt-0.5 size-3.5 shrink-0" />
    <span className="selectable min-w-0 break-words">{text}</span>
  </p>;
}
