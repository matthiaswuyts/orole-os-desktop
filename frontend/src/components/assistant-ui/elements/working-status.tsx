import { useEffect, useState, type ComponentProps } from "react";
import { cn } from "@/lib/utils";

/** Seconds as "12s" or "1m 05s". */
export function formatElapsed(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  return `${Math.floor(seconds / 60)}m ${String(seconds % 60).padStart(2, "0")}s`;
}

/** Whole seconds since `since` (Unix ms), ticking once a second. 0 when there is no start time. */
function useElapsedSeconds(since: number | undefined): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  return since ? Math.max(0, Math.floor((now - since) / 1000)) : 0;
}

/** After this many seconds the line also says how long the work has been going, so a slow step never looks stuck. */
const SHOW_ELAPSED_AFTER = 4;

/**
 * The "the assistant is working" line at the end of a reply that is still being produced: a pulsing dot, a shimmering
 * label that says what it is doing right now ("Searching the web", "Writing the reply") and how long it has been at it.
 * It is a polite live region, so a screen reader hears each change of step.
 */
export function WorkingStatus({ label, since, className, ...props }: Omit<ComponentProps<"div">, "children"> & {
  label: string;
  /** When the work started (Unix ms); drives the elapsed time. */
  since?: number;
}) {
  const elapsed = useElapsedSeconds(since);
  return (
    <div role="status" aria-live="polite" data-slot="working-status" className={cn("flex min-w-0 items-center gap-2 py-1 text-xs text-muted-foreground", className)} {...props}>
      <span aria-hidden className="size-1.5 shrink-0 animate-pulse rounded-full bg-primary motion-reduce:animate-none" />
      <span className="cv-shimmer min-w-0 truncate font-medium">{label}…</span>
      {elapsed >= SHOW_ELAPSED_AFTER && <span data-slot="working-elapsed" className="shrink-0 tabular-nums">{formatElapsed(elapsed)}</span>}
    </div>
  );
}
