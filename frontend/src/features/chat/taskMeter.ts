import type { Session } from "@/mygo";

/** Shown wherever a reading is not known (yet), so a missing value never breaks the layout. */
export const NO_VALUE = "—";

/** Whether the task's clock is still running; every other status has stopped it. */
export const isTaskLive = (session: Session): boolean => session.status === "working";

const time = (iso?: string): number | undefined => {
  const ms = iso ? Date.parse(iso) : NaN;
  return Number.isFinite(ms) ? ms : undefined;
};

/**
 * Whole seconds the task has run: up to `now` while it works, frozen at its end (or last update) once it stopped.
 * Undefined when the daemon reported no usable start time.
 */
export function taskElapsedSeconds(session: Session, now: number): number | undefined {
  const start = time(session.startedAt);
  if (start === undefined) return undefined;
  const end = isTaskLive(session) ? now : time(session.endedAt) ?? time(session.updatedAt) ?? now;
  return Math.max(0, Math.floor((end - start) / 1000));
}

const COMPACT = new Intl.NumberFormat("en", { notation: "compact", maximumFractionDigits: 1 });

/** "842", "12.4K", "1.3M". */
export const formatTokens = (n: number): string => COMPACT.format(n);
