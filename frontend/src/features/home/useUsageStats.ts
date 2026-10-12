import { useEffect } from "react";
import { create } from "zustand";
import type { WidgetStatus } from "@/components/widgets/Widget";
import { inDesktop } from "@/lib/backend";
import { Agents, events, type UsageStats } from "@/mygo";
import { useAgents } from "@/stores/agents";
import { useDaemon } from "@/stores/daemon";

/** The usage windows the daemon summarises; "24h" also picks the default By agent range. */
export type UsageRange = "24h" | "7d" | "all";

/** Sessions the Latest sessions widget can list. */
const LATEST = 12;
/** At most one request per range this often, however many session events arrive. */
export const MIN_GAP_MS = 15_000;
/** Refetch period while Home is visible, so usage outside this app's sessions still shows up. */
export const POLL_MS = 60_000;

interface Entry {
  stats: UsageStats | null;
  error: string | null;
}

/** Usage replies per By agent range. Every other part of a reply is the same for each range. */
export const useUsageStore = create<{ entries: Partial<Record<UsageRange, Entry>> }>(() => ({ entries: {} }));

interface Slot {
  consumers: number;
  inflight: boolean;
  /** Another refresh was asked for while a request was in flight. */
  again: boolean;
  startedAt: number;
  trailing: number | undefined;
}

const slots = new Map<UsageRange, Slot>();
let unwatch: (() => void) | null = null;

function fetchNow(range: UsageRange, slot: Slot) {
  slot.inflight = true;
  slot.startedAt = Date.now();
  Agents.usage({ byAgentRange: range, latest: LATEST }).then(
    (stats): Entry => ({ stats, error: null }),
    (e: unknown): Entry => ({ stats: useUsageStore.getState().entries[range]?.stats ?? null, error: e instanceof Error ? e.message : String(e) }),
  ).then((entry) => useUsageStore.setState((s) => ({ entries: { ...s.entries, [range]: entry } }))).finally(() => {
    slot.inflight = false;
    if (slot.again) {
      slot.again = false;
      refreshUsage(range);
    }
  });
}

/**
 * Refreshes one range for its mounted widgets. Requests share one in-flight call and start no more
 * often than every `MIN_GAP_MS`; a refresh asked for sooner runs once at the end of the gap.
 */
export function refreshUsage(range: UsageRange, force = false) {
  const slot = slots.get(range);
  if (!slot || slot.consumers === 0) return;
  if (slot.inflight) { slot.again = true; return; }
  const wait = slot.startedAt + MIN_GAP_MS - Date.now();
  if (!force && wait > 0) {
    slot.trailing ??= window.setTimeout(() => { slot.trailing = undefined; refreshUsage(range); }, wait);
    return;
  }
  window.clearTimeout(slot.trailing);
  slot.trailing = undefined;
  fetchNow(range, slot);
}

const refreshAll = () => { for (const range of slots.keys()) refreshUsage(range); };

/** Session events (any update or delta) and the daemon reconnecting refresh usage, as does a 60 s poll while the window is visible. */
function watch(): () => void {
  const offAgents = useAgents.subscribe((state, prev) => { if (state.sessions !== prev.sessions) refreshAll(); });
  const offDaemon = events.daemonState.on((link) => { if (link.connected) refreshAll(); });
  const onVisible = () => { if (document.visibilityState === "visible") refreshAll(); };
  document.addEventListener("visibilitychange", onVisible);
  const timer = window.setInterval(() => { if (document.visibilityState === "visible") refreshAll(); }, POLL_MS);
  return () => {
    offAgents();
    offDaemon();
    document.removeEventListener("visibilitychange", onVisible);
    window.clearInterval(timer);
  };
}

function retain(range: UsageRange): () => void {
  let slot = slots.get(range);
  if (!slot) slots.set(range, slot = { consumers: 0, inflight: false, again: false, startedAt: Number.NEGATIVE_INFINITY, trailing: undefined });
  slot.consumers++;
  unwatch ??= watch();
  // A widget mounting beside one that just loaded reuses its reply instead of asking again.
  if (!slot.inflight && Date.now() - slot.startedAt >= MIN_GAP_MS) fetchNow(range, slot);
  return () => {
    if (--slot.consumers > 0) return;
    window.clearTimeout(slot.trailing);
    slot.trailing = undefined;
    slot.again = false;
    if ([...slots.values()].every((s) => s.consumers === 0)) { unwatch?.(); unwatch = null; }
  };
}

export interface UsageResult {
  stats: UsageStats | null;
  status: WidgetStatus;
  message?: string;
  retry?: () => void;
}

/** The shared `agents.usage` reply for Home's usage widgets, with the shared Widget status. */
export function useUsageStats(range: UsageRange = "24h"): UsageResult {
  useEffect(() => (inDesktop ? retain(range) : undefined), [range]);
  const entry = useUsageStore((s) => s.entries[range]);
  const link = useDaemon((s) => s.link);
  if (!inDesktop) return { stats: null, status: "offline", message: "Open Orole OS desktop to see token usage and spend." };
  if (link === "offline") return { stats: null, status: "offline", message: "Daemon offline. Reconnect to load usage." };
  if (entry?.stats) return { stats: entry.stats, status: "ready" };
  if (entry?.error) return { stats: null, status: "error", message: entry.error, retry: () => refreshUsage(range, true) };
  return { stats: null, status: "loading" };
}
