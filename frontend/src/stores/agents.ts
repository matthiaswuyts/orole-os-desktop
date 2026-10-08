import { useMemo } from "react";
import { create } from "zustand";
import { Agents, events, type Detection, type Run, type Session } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { groupColumns, type Columns } from "@/features/agents/columns";

export type SessionMap = Record<string, Session>;

/** Sidebar / status bar numbers. `needsYou` includes failed sessions, like the dashboard column. */
export interface AgentCounts {
  needsYou: number;
  working: number;
  done: number;
  idle: number;
  /** Sessions on the dashboard (discarded ones excluded). */
  total: number;
  /** Finished work (done or failed) nobody has approved or discarded yet. */
  reviewable: number;
}

const ts = (s: { updatedAt: string }) => Date.parse(s.updatedAt) || 0;

/** Upserts a session; an update older than what is held is dropped (events can arrive out of order). */
export function applySessionUpdate(sessions: SessionMap, next: Session): SessionMap {
  const cur = sessions[next.id];
  if (cur && ts(cur) > ts(next)) return sessions;
  return { ...sessions, [next.id]: next };
}

export function removeSession(sessions: SessionMap, id: string): SessionMap {
  if (!(id in sessions)) return sessions;
  const { [id]: _gone, ...rest } = sessions;
  return rest;
}

/**
 * Replaces the held sessions by a fresh List result. Sessions an event delivered while the list
 * was in flight (`live`) are kept, and a held session newer than the listed one wins.
 */
export function mergeList(held: SessionMap, listed: Session[], live: ReadonlySet<string> = new Set()): SessionMap {
  const out: SessionMap = {};
  for (const s of listed) {
    const current = held[s.id];
    out[s.id] = current && ts(current) >= ts(s) ? current : s;
  }
  for (const id of live) {
    if (held[id]) out[id] = held[id];
    else delete out[id];
  }
  return out;
}

const runTs = (r: Run) => Date.parse(r.updatedAt) || 0;

/** Upserts a run, newest first. */
export function applyRunUpdate(runs: Run[], next: Run): Run[] {
  const cur = runs.find((r) => r.id === next.id);
  if (cur && runTs(cur) > runTs(next)) return runs;
  return [next, ...runs.filter((r) => r.id !== next.id)].sort((a, b) => runTs(b) - runTs(a));
}

export const selectColumns = (sessions: SessionMap): Columns => groupColumns(Object.values(sessions));

export const isReviewable = (s: Session) => (s.status === "done" || s.status === "failed") && s.review === "";

export function selectCounts(sessions: SessionMap): AgentCounts {
  const cols = selectColumns(sessions);
  const needsYou = cols["needs-you"].length, working = cols.working.length, done = cols.done.length, idle = cols.idle.length;
  return { needsYou, working, done, idle, total: needsYou + working + done + idle, reviewable: Object.values(sessions).filter(isReviewable).length };
}

const msg = (e: unknown) => (e instanceof Error ? e.message : String(e));

interface State {
  /** False in the browser preview: no daemon, so no agents. */
  available: boolean;
  sessions: SessionMap;
  runs: Run[];
  detections: Detection[] | null;
  detecting: boolean;
  detectError: string | null;
  loaded: boolean;
  loading: boolean;
  error: string | null;
  /** The last List failed: the daemon could not be reached. */
  offline: boolean;
  /** The Start agent dialog is open. */
  startDialogOpen: boolean;
  /** Session shown in the dashboard's detail panel. */
  openId: string | null;
  setStartDialogOpen: (o: boolean) => void;
  setOpenId: (id: string | null) => void;
  load: () => Promise<void>;
  refresh: () => Promise<void>;
  detect: (refresh: boolean) => Promise<void>;
  /** Applies a session returned by a call without waiting for its event. */
  upsert: (s: Session) => void;
  /** Subscribes to daemon events once (reference counted). Returns the disposer. */
  init: () => () => void;
}

let reloadQueued = false;
/** Ids that arrived by event while a List was in flight. */
let liveDuringLoad: Set<string> | null = null;
let subscribers = 0;
let unsubscribe: (() => void) | null = null;

export const useAgents = create<State>((set, get) => ({
  available: inDesktop,
  sessions: {},
  runs: [],
  detections: null,
  detecting: false,
  detectError: null,
  loaded: !inDesktop,
  loading: false,
  error: null,
  offline: false,
  startDialogOpen: false,
  openId: null,
  setStartDialogOpen: (startDialogOpen) => set({ startDialogOpen }),
  setOpenId: (openId) => set({ openId }),

  async load() {
    if (!inDesktop) return;
    if (get().loading) { reloadQueued = true; return; }
    set({ loading: true });
    liveDuringLoad = new Set();
    try {
      const [list, runs] = await Promise.all([Agents.list(), Agents.runs()]);
      set((s) => ({ sessions: mergeList(s.sessions, list, liveDuringLoad ?? undefined), runs: runs.reduce(applyRunUpdate, s.runs), loaded: true, loading: false, error: null, offline: false }));
    } catch (e) {
      set({ loaded: true, loading: false, error: msg(e), offline: true });
    }
    liveDuringLoad = null;
    if (reloadQueued) { reloadQueued = false; await get().load(); }
  },

  refresh: () => get().load(),

  async detect(refresh) {
    if (!inDesktop) return;
    set({ detecting: true });
    try {
      set({ detections: await Agents.detect(refresh), detecting: false, detectError: null });
    } catch (e) {
      set({ detecting: false, detectError: msg(e) });
    }
  },

  upsert: (s) => { liveDuringLoad?.add(s.id); set((st) => ({ sessions: applySessionUpdate(st.sessions, s) })); },

  init() {
    if (!inDesktop) return () => {};
    if (subscribers++ === 0) {
      const offs = [
        events.agentsUpdated.on((s) => { liveDuringLoad?.add(s.id); set((st) => ({ sessions: applySessionUpdate(st.sessions, s) })); }),
        events.agentsRemoved.on((r) => { liveDuringLoad?.add(r.id); set((st) => ({ sessions: removeSession(st.sessions, r.id), openId: st.openId === r.id ? null : st.openId })); }),
        events.agentsRun.on((r) => set((st) => ({ runs: applyRunUpdate(st.runs, r) }))),
        events.daemonState.on((l) => { if (l.connected) void get().load(); else set({ offline: true }); }),
      ];
      unsubscribe = () => offs.forEach((o) => o());
      void get().load();
    }
    let disposed = false;
    return () => {
      if (disposed) return;
      disposed = true;
      if (--subscribers === 0) { unsubscribe?.(); unsubscribe = null; }
    };
  },
}));

/** Counts for the sidebar badges and the status bar, recomputed only when sessions change. */
export function useAgentCounts(): AgentCounts {
  const sessions = useAgents((s) => s.sessions);
  return useMemo(() => selectCounts(sessions), [sessions]);
}

export function useAgentColumns(): Columns {
  const sessions = useAgents((s) => s.sessions);
  return useMemo(() => selectColumns(sessions), [sessions]);
}
