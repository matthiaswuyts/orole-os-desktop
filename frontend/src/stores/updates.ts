import { create } from "zustand";
import { Updates, type UpdateStatus } from "@/mygo";
import { inDesktop } from "@/lib/backend";

interface State {
  status: UpdateStatus | null;
  error: string | null;
  busy: "check" | "install" | null;
  check: () => Promise<void>;
  install: () => Promise<void>;
  startSync: () => () => void;
}
const interval = 30 * 60 * 1000;
let lastChecked = 0;
let subscribers = 0;
let stopSync: (() => void) | undefined;

export const useUpdates = create<State>((set, get) => ({
  status: null, error: null, busy: null,
  async check() {
    if (!inDesktop || get().busy) return;
    set({ busy: "check", error: null });
    try {
      const status = await Updates.check();
      lastChecked = Date.now();
      set({ status, error: status.error || null });
    } catch (error) { set({ error: error instanceof Error ? error.message : String(error) }); }
    finally { set({ busy: null }); }
  },
  async install() {
    const state = get();
    if (!inDesktop || state.busy || !state.status?.enabled || !state.status.available) return;
    set({ busy: "install", error: null });
    try {
      const status = await Updates.install(true);
      set({ status, error: status.error || null });
    } catch (error) { set({ error: error instanceof Error ? error.message : String(error) }); }
    finally { set({ busy: null }); }
  },
  startSync() {
    if (!inDesktop) return () => {};
    if (++subscribers === 1) {
      const refresh = () => {
        if (!document.hidden && (!lastChecked || Date.now() - lastChecked >= interval)) void get().check();
      };
      const timer = window.setInterval(refresh, interval);
      document.addEventListener("visibilitychange", refresh);
      refresh();
      stopSync = () => { window.clearInterval(timer); document.removeEventListener("visibilitychange", refresh); };
    }
    let stopped = false;
    return () => {
      if (stopped) return;
      stopped = true;
      if (--subscribers === 0) { stopSync?.(); stopSync = undefined; }
    };
  },
}));
