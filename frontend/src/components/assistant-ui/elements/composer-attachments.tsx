/*
 * Adapted from assistant-ui ComposerAttachments / ComposerAttachmentChip,
 * commit 9201447608271c64a8a9ca66c69e81d27a1d9fab. See docs/research/upstream-attribution.md.
 * MIT License — Copyright (c) 2026 AgentbaseAI Inc.
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 * The above copyright notice and this permission notice shall be included in all
 * copies or substantial portions of the Software.
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */
import type { ComponentProps } from "react";
import { CheckIcon, FileArchiveIcon, FileImageIcon, FileTextIcon, Loader2Icon, XIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export interface ComposerAttachment {
  name: string;
  meta: string;
  state: "uploading" | "done" | "error";
  kind?: "image" | "text" | "archive";
}
const ATTACHMENT_ICONS = { image: FileImageIcon, text: FileTextIcon, archive: FileArchiveIcon };

export function ComposerAttachments({ className, ...props }: ComponentProps<"div">) {
  return <div data-slot="composer-attachments" className={cn("flex flex-wrap gap-2", className)} {...props} />;
}

export function ComposerAttachmentChip({ attachment, onRemove, className, ...props }: Omit<ComponentProps<"div">, "children"> & {
  attachment: ComposerAttachment;
  onRemove?: (name: string) => void;
}) {
  const Icon = ATTACHMENT_ICONS[attachment.kind ?? "text"];
  return <div data-slot="composer-attachment" data-state={attachment.state}
    className={cn("relative flex max-w-full items-center gap-2.5 rounded-xl border bg-muted/40 py-1.5 ps-1.5 pe-2", className)} {...props}>
    <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-background text-muted-foreground"><Icon aria-hidden className="size-4" /></span>
    <span className="flex min-w-0 flex-col">
      <span className="max-w-36 truncate text-xs font-medium">{attachment.name}</span>
      <span role={attachment.state === "error" ? "alert" : "status"} className={cn("max-w-56 text-xs", attachment.state === "error" ? "text-destructive" : "text-muted-foreground")}>{attachment.meta}</span>
    </span>
    <span className="ms-1 flex shrink-0 items-center gap-1">
      {attachment.state === "uploading" && <Loader2Icon aria-hidden className="size-3.5 animate-spin text-muted-foreground motion-reduce:animate-none" />}
      {onRemove ? <Button type="button" variant="ghost" size="icon-xs" aria-label={`Remove ${attachment.name}`} onClick={() => onRemove(attachment.name)}><XIcon /></Button>
        : attachment.state === "done" && <CheckIcon aria-hidden className="size-3.5 text-success" />}
    </span>
  </div>;
}
