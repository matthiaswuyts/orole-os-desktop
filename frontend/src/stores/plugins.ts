import { create } from "zustand";
import { Plugins, events, type PluginRecord } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { useDaemon } from "@/stores/daemon";

interface State {
  records: PluginRecord[];
  staleIds: string[];
  ready: boolean;
  loading: boolean;
  error: string | null;
  load: () => Promise<void>;
  startSync: () => () => void;
}

let sequence = 0;
let subscribers = 0;
let stop: (() => void) | undefined;

/** Only the daemon owns manifests and trust. This is a disposable, event-synchronized snapshot. */
export const usePlugins = create<State>((set, get) => ({
  records: [], staleIds: [], ready: false, loading: false, error: null,
  async load() {
    if (!inDesktop) return;
    const request = ++sequence;
    set({ loading: true, error: null });
    try {
      const records = await Plugins.list();
      if (request === sequence) set({ records: records ?? [], staleIds: [], ready: true, loading: false, error: null });
    } catch (cause) {
      if (request === sequence) set({ ready: false, loading: false, error: cause instanceof Error ? cause.message : String(cause) });
    }
  },
  startSync() {
    if (!inDesktop) return () => {};
    if (++subscribers === 1) {
      const refresh = () => { void get().load(); };
      // Subscribe first; each notification invalidates older in-flight list responses.
      const offChanged = events.pluginsChanged.on(({ id }) => {
        set((state) => ({ staleIds: [...state.staleIds.filter((value) => value !== id), id] }));
        refresh();
      });
      const offDaemon = useDaemon.subscribe((state, previous) => {
        if (state.link === "connected" && previous.link !== "connected") refresh();
        if (state.link === "offline" && previous.link !== "offline") {
          ++sequence;
          set({ ready: false, loading: false, error: "The daemon is offline. Reconnect to load or run plugins." });
        }
      });
      window.addEventListener("focus", refresh);
      refresh();
      stop = () => { offChanged(); offDaemon(); window.removeEventListener("focus", refresh); };
    }
    let stopped = false;
    return () => {
      if (stopped) return;
      stopped = true;
      if (--subscribers === 0) {
        ++sequence;
        stop?.();
        stop = undefined;
        set({ ready: false, loading: false });
      }
    };
  },
}));

export function isPluginActive(record: PluginRecord) {
  return record.enabled && record.digest !== "" && record.digest === record.approvedDigest;
}
