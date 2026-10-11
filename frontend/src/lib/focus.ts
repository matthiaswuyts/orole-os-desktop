/**
 * One shared "window regained focus" signal for stores that re-sync on focus.
 *
 * Every store used to add its own `focus` listener and reload on every alt-tab, which fanned out into a
 * burst of daemon calls. This module owns the single listener and drops focus events that arrive sooner
 * than `FOCUS_MIN_INTERVAL_MS` after the last one it delivered; daemon events keep data live in between.
 */
export const FOCUS_MIN_INTERVAL_MS = 30_000;

const listeners = new Set<() => void>();
let lastEmit = 0;

function onWindowFocus() {
  const now = Date.now();
  if (now - lastEmit < FOCUS_MIN_INTERVAL_MS) return;
  lastEmit = now;
  for (const listener of [...listeners]) {
    try { listener(); } catch (error) { console.error("focus refresh failed", error); }
  }
}

/** Calls `listener` when the window regains focus, at most once per `FOCUS_MIN_INTERVAL_MS` across all subscribers. Returns the unsubscribe. */
export function onFocusRefresh(listener: () => void): () => void {
  if (listeners.size === 0) {
    // Subscribing means the caller just loaded; a focus event moments later has nothing new to fetch.
    lastEmit = Date.now();
    window.addEventListener("focus", onWindowFocus);
  }
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0) window.removeEventListener("focus", onWindowFocus);
  };
}
