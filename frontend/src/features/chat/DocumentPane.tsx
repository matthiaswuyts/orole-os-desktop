import { FileTextIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { CanvasSplitBody, CanvasSplitDocument, CanvasSplitHeader } from "@/components/assistant-ui/elements/canvas-split";
import { Markdown } from "@/components/Markdown";
import { selectConversation, useChatMessages } from "@/stores/chatMessages";
import { parseDocument } from "./parts/DocumentCard";

/**
 * A document a reply wrote, read beside the conversation: the part's markdown in a canvas document, still "writing"
 * while the reply that holds it runs. Rendered by the side pane for a `document` pane.
 */
export function DocumentPane({ botId, messageId, partIndex, onClose }: { botId: string; messageId: string; partIndex: number; onClose: () => void }) {
  const message = useChatMessages((s) => selectConversation(botId)(s).messages.find((m) => m.id === messageId));
  const part = message?.parts[partIndex];
  const doc = part?.type === "ui" && part.name === "document" ? parseDocument(part.spec) : null;
  const [copied, setCopied] = useState(false);
  const timer = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  const copy = async () => {
    if (!doc) return;
    try { await navigator.clipboard.writeText(doc.content); setCopied(true); } catch { setCopied(false); }
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setCopied(false), 1800);
  };
  const title = doc?.title ?? "Document";
  return <section aria-labelledby="side-pane-title" data-slot="document-pane" className="flex h-full min-w-0 flex-col">
    <CanvasSplitDocument>
      <CanvasSplitHeader title={title} version={doc?.version ?? 1} saved={message?.status !== "running"} onCopy={doc ? () => void copy() : undefined} onClose={onClose}>
        <FileTextIcon aria-hidden className="size-4 shrink-0 text-muted-foreground" />
        <h2 id="side-pane-title" tabIndex={-1} className="sr-only">{title}</h2>
        {copied && <span role="status" className="sr-only">Copied</span>}
      </CanvasSplitHeader>
      <CanvasSplitBody writing={message?.status === "running"}>
        {doc
          ? <Markdown>{doc.content}</Markdown>
          : <p role="status" className="p-3 text-center text-sm text-muted-foreground">This document is no longer in the conversation.</p>}
      </CanvasSplitBody>
    </CanvasSplitDocument>
  </section>;
}
