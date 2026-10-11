import { ArrowRightIcon } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";
import { cn } from "@/lib/utils";
import { surfaces } from "./surfaces";

/**
 * assistant-ui's agent handoff (https://www.assistant-ui.com/elements/agent-handoff): control passing from one agent to
 * another, with the reason and what came along. Presentational: in transit the arrow and the `to` pill read blue; once
 * `settled` the `from` pill dims and the `to` pill goes neutral because it is now the current speaker.
 */
export function AgentHandoff({ from, to, fromAvatar, toAvatar, reason, carried, settled, className, ...props }: ComponentProps<"div"> & {
  from: string;
  to: string;
  /** Optional avatars drawn inside the pills (an extension of the element). */
  fromAvatar?: ReactNode;
  toAvatar?: ReactNode;
  reason: string;
  carried: readonly string[];
  settled: boolean;
}) {
  return (
    <div data-slot="agent-handoff" data-settled={settled || undefined} className={cn("flex min-w-0 flex-col gap-1.5 text-xs", className)} {...props}>
      <div className="flex min-w-0 items-center gap-2">
        <span className={cn(surfaces.field, "inline-flex max-w-[45%] items-center gap-1.5 px-2 py-0.5 transition-opacity duration-500", settled && "opacity-60")}>{fromAvatar}<span className="truncate">{from}</span></span>
        <ArrowRightIcon aria-hidden className={cn("size-3.5 shrink-0 transition-colors duration-500", settled ? "text-muted-foreground" : "text-info-foreground")} />
        <span className={cn(surfaces.field, "inline-flex max-w-[45%] items-center gap-1.5 px-2 py-0.5 transition-colors duration-500", !settled && "border-info/40 bg-info/10 text-info-foreground")}>{toAvatar}<span className="truncate">{to}</span></span>
      </div>
      <p className="text-muted-foreground">{reason}</p>
      {carried.length > 0 && <div className="flex flex-col gap-0.5">
        <span className={surfaces.mono}>carried over</span>
        {carried.map((item) => <span key={item} className="truncate text-muted-foreground" title={item}>{item}</span>)}
      </div>}
    </div>
  );
}
