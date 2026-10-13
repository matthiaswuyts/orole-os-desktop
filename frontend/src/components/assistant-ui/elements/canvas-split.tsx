/*
 * Orole's own implementation of assistant-ui's canvas split (https://www.assistant-ui.com/elements/canvas-split),
 * written from the published documentation (parts, props, anatomy); nothing is copied. See docs/research/upstream-attribution.md.
 */
import { CheckIcon, CopyIcon, PenLineIcon, XIcon } from "lucide-react";
import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";
import { ghostButton, mono } from "./surfaces";

/** Root: the thread rail and the document stack below `@xl` of its own width and sit side by side from there up. */
export function CanvasSplit({ className, ...props }: ComponentProps<"div">) {
  return <div data-slot="canvas-split" className={cn("@container flex h-full min-h-0 w-full min-w-0 flex-col @xl:flex-row", className)} {...props} />;
}

export function CanvasSplitThread({ className, ...props }: ComponentProps<"div">) {
  return <div data-slot="canvas-split-thread" className={cn("flex min-h-0 min-w-0 flex-col gap-2 overflow-y-auto p-3 @xl:w-72 @xl:shrink-0 @xl:border-r", className)} {...props} />;
}

export function CanvasSplitMessage({ speaker, className, ...props }: ComponentProps<"div"> & { speaker: "user" | "assistant" }) {
  return <div data-slot="canvas-split-message" data-speaker={speaker}
    className={cn("text-sm leading-relaxed", speaker === "user" ? "ml-auto w-fit max-w-[85%] rounded-2xl rounded-br-md bg-primary px-3 py-1.5 text-primary-foreground" : "text-foreground", className)} {...props} />;
}

/** The document column. */
export function CanvasSplitDocument({ className, ...props }: ComponentProps<"div">) {
  return <div data-slot="canvas-split-document" className={cn("flex min-h-0 min-w-0 flex-1 flex-col bg-background", className)} {...props} />;
}

/**
 * Title, version, saved-or-writing state, copy and close. A missing handler disables its button rather than hiding it,
 * so the header keeps its width whether or not an action is available.
 */
export function CanvasSplitHeader({ title, version, saved, onCopy, onClose, className, children, ...props }: Omit<ComponentProps<"div">, "title"> & {
  title: string;
  version: number;
  saved: boolean;
  onCopy?: () => void;
  onClose?: () => void;
}) {
  return <div data-slot="canvas-split-header" className={cn("flex h-10 shrink-0 items-center gap-2 border-b px-3", className)} {...props}>
    {children}
    <span className="min-w-0 flex-1 truncate text-sm font-medium" title={title}>{title}</span>
    <span className={cn(mono, "shrink-0")}>v{version}</span>
    <span data-saved={saved} className={cn("flex shrink-0 items-center gap-1 text-xs", saved ? "text-muted-foreground" : "text-info")}>
      {saved ? <CheckIcon aria-hidden className="size-3" /> : <PenLineIcon aria-hidden className="size-3 animate-pulse motion-reduce:animate-none" />}
      {saved ? "Saved" : "Writing"}
    </span>
    <button type="button" aria-label="Copy document" disabled={!onCopy} onClick={onCopy} className={cn(ghostButton, "size-6 disabled:opacity-40 disabled:hover:bg-transparent")}><CopyIcon aria-hidden className="size-3.5" /></button>
    <button type="button" aria-label="Close document" disabled={!onClose} onClick={onClose} className={cn(ghostButton, "size-6 disabled:opacity-40 disabled:hover:bg-transparent")}><XIcon aria-hidden className="size-3.5" /></button>
  </div>;
}

/** The lines, with a blinking caret after the last one while `writing`. */
export function CanvasSplitBody({ writing = false, className, children, ...props }: ComponentProps<"div"> & { writing?: boolean }) {
  return <div data-slot="canvas-split-body" data-writing={writing || undefined} className={cn("min-h-0 flex-1 overflow-y-auto px-4 py-3", className)} {...props}>
    {children}
    {writing && <span aria-hidden className="ml-0.5 inline-block h-4 w-0.5 animate-caret-blink bg-foreground align-text-bottom motion-reduce:animate-none" />}
  </div>;
}

export function CanvasSplitLine({ heading = false, className, ...props }: ComponentProps<"p"> & { heading?: boolean }) {
  return <p data-slot="canvas-split-line" className={cn("text-sm leading-relaxed", heading ? "mt-3 font-semibold text-foreground first:mt-0" : "text-foreground/85", className)} {...props} />;
}
