import { FileTextIcon, PanelRightOpenIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { paper } from "@/components/assistant-ui/elements/surfaces";
import { cn } from "@/lib/utils";
import { CopyButton } from "./Sources";

/** The `document` ui part the daemon's write_document tool emits: a titled markdown document, versioned per rewrite. */
export interface DocumentSpec { title: string; content: string; version?: number }

/** The document a `document` ui part holds; null when the spec is not one. */
export function parseDocument(spec: string | undefined): DocumentSpec | null {
  if (!spec) return null;
  try {
    const value: unknown = JSON.parse(spec);
    if (typeof value !== "object" || value === null) return null;
    const { title, content, version } = value as Record<string, unknown>;
    return typeof title === "string" && typeof content === "string" ? { title, content, version: typeof version === "number" ? version : undefined } : null;
  } catch { return null; }
}

/** Words in a markdown document, for the card's size hint; bare markers (`#`, `-`, `*`) are not words. */
export const wordCount = (text: string): number => text.split(/\s+/).filter((token) => /[\p{L}\p{N}]/u.test(token)).length;

/** A document a reply wrote, as one compact line in the thread: open it beside the conversation or copy its markdown. */
export function DocumentCard({ spec, open, onOpen }: { spec: DocumentSpec; /** whether this document is the one open beside the conversation */ open: boolean; /** absent = nowhere to open it (a preview outside a conversation) */ onOpen?: () => void }) {
  const words = wordCount(spec.content);
  return <div data-slot="document-card" data-open={open || undefined} className={cn(paper, "flex w-full max-w-md min-w-0 items-center gap-2 px-2.5 py-1.5", open && "border-ring/60")}>
    <FileTextIcon aria-hidden className="size-4 shrink-0 text-muted-foreground" />
    <div className="flex min-w-0 flex-1 flex-col">
      <span className="truncate text-sm leading-5 font-medium">{spec.title}</span>
      <span className="text-xs leading-4 text-muted-foreground">v{spec.version ?? 1} · {words} {words === 1 ? "word" : "words"}</span>
    </div>
    <Button type="button" variant="outline" size="xs" aria-pressed={open} disabled={!onOpen} onClick={onOpen}><PanelRightOpenIcon aria-hidden />Open</Button>
    <CopyButton text={spec.content} label="Copy document" />
  </div>;
}
