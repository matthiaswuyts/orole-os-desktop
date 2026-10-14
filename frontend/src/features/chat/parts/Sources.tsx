import { CheckIcon, CopyIcon, ExternalLinkIcon, FileIcon, FileImageIcon, FileTextIcon, LinkIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { bubble } from "@/components/assistant-ui/elements/surfaces";
import { Markdown } from "@/components/Markdown";
import { Button } from "@/components/ui/button";
import { Popover, PopoverPopup, PopoverTrigger } from "@/components/ui/popover";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { isWebURL, openExternal } from "@/lib/openExternal";
import { cn } from "@/lib/utils";
import { formatSize, hostOf, type Source } from "./format";

/**
 * Copies text to the clipboard and says so; it never claims success when the write failed. With `html` the clipboard
 * also gets a rich version (a real table when pasted into a document), and plain-text targets still get `text`.
 */
export function CopyButton({ text, html, label, className }: { text: string; html?: string; label: string; className?: string }) {
  const [state, setState] = useState<"idle" | "copied" | "failed">("idle");
  const timer = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  const copy = async () => {
    try {
      if (html && typeof ClipboardItem !== "undefined") {
        await navigator.clipboard.write([new ClipboardItem({ "text/plain": new Blob([text], { type: "text/plain" }), "text/html": new Blob([html], { type: "text/html" }) })]);
      } else {
        await navigator.clipboard.writeText(text);
      }
      setState("copied");
    } catch { setState("failed"); }
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setState("idle"), 1800);
  };
  const name = state === "copied" ? "Copied" : state === "failed" ? "Copy failed" : label;
  return <Tooltip>
    <TooltipTrigger render={<Button type="button" variant="ghost" size="icon-xs" className={className} aria-label={name} disabled={!text} onClick={() => void copy()} />}>
      {state === "copied" ? <CheckIcon aria-hidden /> : <CopyIcon aria-hidden />}
    </TooltipTrigger>
    <TooltipPopup>{name}</TooltipPopup>
  </Tooltip>;
}

/** "3 sources": opens the list of citations; only http(s) links are clickable. */
export function SourcesButton({ sources, className }: { sources: readonly Source[]; className?: string }) {
  if (!sources.length) return null;
  return <Popover>
    <PopoverTrigger render={<Button type="button" variant="outline" size="xs" className={cn("self-start rounded-full", className)} />}>
      <LinkIcon aria-hidden />
      <span>{sources.length} {sources.length === 1 ? "source" : "sources"}</span>
    </PopoverTrigger>
    <PopoverPopup align="start" className="w-80 [--viewport-inline-padding:--spacing(1)]" aria-label="Sources">
      <ol className="flex w-full min-w-0 flex-col py-0 text-xs">
        {sources.map((source, index) => <li key={source.url} className="min-w-0">
          {isWebURL(source.url)
            ? <a href={source.url} target="_blank" rel="noopener noreferrer" className="flex min-w-0 gap-2 rounded-md px-2 py-1.5 outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring"
              onClick={(event) => { event.preventDefault(); void openExternal(source.url); }}>
              <SourceBody index={index} source={source} link />
            </a>
            : <div className="flex min-w-0 gap-2 px-2 py-1.5"><SourceBody index={index} source={source} /></div>}
        </li>)}
      </ol>
    </PopoverPopup>
  </Popover>;
}

function SourceBody({ index, source, link }: { index: number; source: Source; link?: boolean }) {
  return <>
    <span className="mt-0.5 w-4 shrink-0 text-end text-muted-foreground tabular-nums">{index + 1}</span>
    <span className="flex min-w-0 flex-col">
      <span className="flex min-w-0 items-center gap-1 font-medium"><span className="truncate">{source.title}</span>{link && <ExternalLinkIcon aria-hidden className="size-3 shrink-0 text-muted-foreground" />}</span>
      <span className="truncate text-muted-foreground">{hostOf(source.url)}</span>
      {source.snippet && <span className="line-clamp-2 text-muted-foreground">{source.snippet}</span>}
    </span>
  </>;
}

/** A research report artifact: titled markdown with its sources and a copy action. */
export function ReportCard({ title, text, sources }: { title?: string; text: string; sources: readonly Source[] }) {
  return <section data-slot="chat-report" aria-label={title || "Research report"} className={cn(bubble, "flex min-w-0 flex-col gap-2 p-3")}>
    <header className="flex items-center gap-2">
      <FileTextIcon aria-hidden className="size-4 shrink-0 text-muted-foreground" />
      <h3 className="min-w-0 flex-1 truncate text-sm font-semibold">{title || "Research report"}</h3>
      <CopyButton text={text} label="Copy report" />
    </header>
    <Markdown>{text}</Markdown>
    <SourcesButton sources={sources} />
  </section>;
}

/** A sent file in a user message: name and size, nothing clickable (bytes only load on demand elsewhere). */
export function AttachmentPill({ name, mime, size }: { name?: string; mime?: string; size?: number }) {
  const Icon = mime?.startsWith("image/") ? FileImageIcon : mime?.startsWith("text/") || mime === "application/pdf" ? FileTextIcon : FileIcon;
  const label = name?.trim() || "Attachment";
  return <span data-slot="chat-attachment" className="inline-flex max-w-56 items-center gap-1.5 rounded-lg border bg-background/60 px-2 py-1 text-xs">
    <Icon aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
    <span className="truncate">{label}</span>
    {size ? <span className="shrink-0 text-muted-foreground tabular-nums">{formatSize(size)}</span> : null}
  </span>;
}
