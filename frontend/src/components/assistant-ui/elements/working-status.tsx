import { useEffect, useState, type ComponentProps } from "react";
import { cn } from "@/lib/utils";

/** Seconds as "12s" or "1m 05s". */
export function formatElapsed(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  return `${Math.floor(seconds / 60)}m ${String(seconds % 60).padStart(2, "0")}s`;
}

/** A step's length: tenths of a second under ten seconds ("0.4s", "5.2s"), then "12s", "2m 05s", "1h 05m". */
export function formatSpan(ms: number): string {
  if (ms < 10_000) return `${(Math.max(0, ms) / 1000).toFixed(1)}s`;
  const seconds = Math.floor(ms / 1000);
  if (seconds < 3600) return formatElapsed(seconds);
  return `${Math.floor(seconds / 3600)}h ${String(Math.floor((seconds % 3600) / 60)).padStart(2, "0")}m`;
}

/** Date.now(), ticking once a second while `live`; a component that is not live keeps the time of its last render. */
export function useNow(live: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!live) return;
    setNow(Date.now());
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [live]);
  return now;
}

/**
 * How long something took (`start` to `end`, Unix ms), or, while `live` and unfinished, how long it has been going, ticking in
 * whole seconds. Renders nothing when the timestamps do not say. Only this leaf re-renders on a tick.
 */
export function Elapsed({ start, end, live = false, className }: { start?: number | null; end?: number | null; live?: boolean; className?: string }) {
  const ticking = live && end == null && start != null;
  const now = useNow(ticking);
  if (start == null) return null;
  const stop = end ?? (ticking ? now : null);
  if (stop == null) return null;
  const ms = Math.max(0, stop - start);
  return <span data-slot="elapsed" className={cn("shrink-0 tabular-nums", className)}>{ticking ? formatElapsed(Math.floor(ms / 1000)) : formatSpan(ms)}</span>;
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
