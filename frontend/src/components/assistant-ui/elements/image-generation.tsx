import { RefreshCwIcon } from "lucide-react";
import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";
import { surfaces } from "./surfaces";

const DOTS = Array.from({ length: 64 }, (_, index) => index);

/**
 * assistant-ui's image generation (https://www.assistant-ui.com/elements/image-generation), standalone lane: a pulsing dot
 * grid holds the frame for an image that has not arrived yet, with a shimmering "Generating" label in place of the prompt.
 * It never receives or renders an image: once the real artifact exists the host swaps in an image renderer. There is no
 * resolution label and no regenerate button unless the host passes `onRegenerate` for an action that really exists.
 */
export function ImageGeneration({ prompt, generating, onRegenerate, className, ...props }: Omit<ComponentProps<"div">, "children"> & {
  prompt: string;
  generating: boolean;
  onRegenerate?: () => void;
}) {
  return (
    <div data-slot="image-generation" data-generating={generating} aria-busy={generating} className={cn("flex w-full max-w-72 min-w-0 flex-col gap-2", className)} {...props}>
      <div aria-hidden className="relative isolate aspect-square w-full overflow-hidden rounded-xl border bg-muted/40">
        <div className={cn("absolute inset-0 -z-10 bg-linear-to-br from-primary/15 via-muted to-info/15 transition-[filter,opacity] duration-700 motion-reduce:transition-none", generating ? "opacity-70 blur-xl" : "opacity-100 blur-0")} />
        <div className={cn("grid size-full grid-cols-8 place-items-center p-4 transition-opacity duration-500 motion-reduce:transition-none", generating ? "opacity-100" : "opacity-0")}>
          {DOTS.map((dot) => <span key={dot} className="size-1 animate-pulse rounded-full bg-foreground/40 motion-reduce:animate-none" style={{ animationDelay: `${((dot % 8) + Math.floor(dot / 8)) * 90}ms` }} />)}
        </div>
      </div>
      <div className="flex min-w-0 items-start gap-2 text-xs">
        {generating
          ? <span role="status" className="cv-shimmer font-medium">Generating</span>
          : <p className={cn(surfaces.mono, "selectable min-w-0 flex-1 break-words")}>{prompt}</p>}
        {generating && prompt && <p className="selectable min-w-0 flex-1 break-words text-muted-foreground">{prompt}</p>}
        {onRegenerate && <button type="button" aria-label="Regenerate image" aria-hidden={generating || undefined} disabled={generating} tabIndex={generating ? -1 : 0} onClick={onRegenerate} className={cn(surfaces.ghostButton, "ms-auto size-6 shrink-0 transition-opacity", generating && "opacity-0")}><RefreshCwIcon aria-hidden className="size-3.5" /></button>}
      </div>
    </div>
  );
}
