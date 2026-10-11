import { create } from "zustand";
import { Daemon, events, type Health } from "@/mygo";
import { inDesktop } from "@/lib/backend";

export type Link = "unknown" | "connected" | "offline" | "preview";

interface DaemonState {
  link: Link;
  health: Health | null;
  error: string | null;
  refreshedAt: number;
  /** Increments each time the daemon becomes reachable again, so caches can drop data from an earlier process. */
  connectEpoch: number;
  refresh: () => Promise<void>;
}

/** The next state for an offline observation; the same state object (so subscribers are not notified) when it changes nothing but the timestamp. */
function offline(state: DaemonState, error: string): DaemonState | Pick<DaemonState, "link" | "error" | "refreshedAt"> {
  return state.link === "offline" && state.error === error ? state : { link: "offline", error, refreshedAt: Date.now() };
}

export const useDaemon = create<DaemonState>((set) => ({
  link: inDesktop ? "unknown" : "preview",
  health: null,
  error: null,
  refreshedAt: 0,
  connectEpoch: 0,
  async refresh() {
    if (!inDesktop) return;
    try {
      const health = await Daemon.health();
      set((state) => {
        if (state.link === "connected" && state.error === null && state.health && (Object.keys(health) as (keyof Health)[]).every((key) => state.health![key] === health[key])) return state;
        return { link: "connected", health, error: null, refreshedAt: Date.now(), connectEpoch: state.link === "connected" ? state.connectEpoch : state.connectEpoch + 1 };
      });
    } catch (e) {
      set((state) => offline(state, e instanceof Error ? e.message : String(e)));
    }
  },
}));

/** Health polling is the only periodic work: 5 s while focused, paused while hidden. */
export function startDaemonPolling(): () => void {
  if (!inDesktop) return () => {};
  const s = useDaemon.getState;
  void s().refresh();
  let t: ReturnType<typeof setInterval> | undefined;
  const arm = () => {
    clearInterval(t);
    if (!document.hidden) t = setInterval(() => void s().refresh(), 5000);
  };
  arm();
  const onVis = () => {
    arm();
    if (!document.hidden) void s().refresh();
  };
  document.addEventListener("visibilitychange", onVis);
  const off = events.daemonState.on((l) => {
    if (l.connected) void s().refresh();
    else useDaemon.setState((state) => offline(state, l.error ?? "disconnected"));
  });
  return () => {
    clearInterval(t);
    document.removeEventListener("visibilitychange", onVis);
    off();
  };
}
