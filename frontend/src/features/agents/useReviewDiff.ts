import { useCallback, useEffect, useRef, useState } from "react";
import { Agents, type ReviewDiff } from "@/mygo";
import { errText } from "./format";

/** Minimum time between the starts of two diff requests for one session; the daemon diffs the worktree on every call. */
export const DIFF_REFRESH_MS = 2000;

interface Result { sessionId: string; diff: ReviewDiff | null; error: string | null }

/**
 * A session's worktree diff, loaded when `enabled` and refreshed when the session changes
 * (`updatedAt`). Refreshes never overlap and start at most once per DIFF_REFRESH_MS; changes that land
 * meanwhile collapse into one trailing refresh. The previous diff stays visible while it refreshes.
 */
export function useReviewDiff(sessionId: string, updatedAt: string, enabled: boolean) {
  const [result, setResult] = useState<Result | null>(null);
  const [version, setVersion] = useState(0);
  const latest = useRef(updatedAt);
  const changed = useRef<() => void>(() => {});

  useEffect(() => { latest.current = updatedAt; changed.current(); }, [updatedAt]);
  useEffect(() => {
    if (!enabled) return;
    let active = true, inFlight = false, timer: number | undefined, startedAt = 0, fetchedFor = latest.current;
    const schedule = () => {
      if (timer === undefined) timer = window.setTimeout(load, Math.max(0, startedAt + DIFF_REFRESH_MS - Date.now()));
    };
    const load = () => {
      timer = undefined;
      inFlight = true;
      startedAt = Date.now();
      fetchedFor = latest.current;
      void Agents.diff(sessionId).then(
        (diff) => { if (active) setResult({ sessionId, diff, error: null }); },
        (error: unknown) => { if (active) setResult({ sessionId, diff: null, error: errText(error) }); },
      ).finally(() => {
        inFlight = false;
        if (active && fetchedFor !== latest.current) schedule();
      });
    };
    changed.current = () => { if (!inFlight && fetchedFor !== latest.current) schedule(); };
    load();
    return () => { active = false; clearTimeout(timer); changed.current = () => {}; };
  }, [enabled, sessionId, version]);

  const retry = useCallback(() => { setResult(null); setVersion((v) => v + 1); }, []);
  const current = result?.sessionId === sessionId ? result : null;
  return { diff: current?.diff ?? null, error: current?.error ?? null, retry };
}
