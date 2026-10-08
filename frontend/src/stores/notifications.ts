import { create } from "zustand";
import { Notifications, events } from "@/mygo";
import type { NotificationList, Prefs, SourcePref } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { useDaemon } from "@/stores/daemon";

const message = (error: unknown) => error instanceof Error ? error.message : String(error);
let listRequest = 0;
let prefsRequest = 0;
let subscribers = 0;
let stopSync: (() => void) | undefined;

interface State {
  items: NotificationList["items"];
  sources: NotificationList["sources"];
  unread: number;
  source: string;
  unreadOnly: boolean;
  loaded: boolean;
  loading: boolean;
  busy: boolean;
  error: string | null;
  actionError: string | null;
  prefs: Prefs | null;
  prefsLoading: boolean;
  prefsSaving: boolean;
  prefsError: string | null;
  nativeError: string | null;
  nativeStatus: string | null;
  testing: boolean;
  testStatus: string | null;
  testError: string | null;
  test: () => Promise<void>;
  filter: (source: string, unreadOnly: boolean) => void;
  load: () => Promise<void>;
  markRead: (ids: string[], all?: boolean) => Promise<void>;
  dismiss: (ids: string[], all?: boolean) => Promise<void>;
  loadPrefs: () => Promise<void>;
  setSourcePref: (source: string | null, pref: SourcePref | null) => Promise<void>;
  startSync: () => () => void;
}

export const useNotifications = create<State>((set, get) => {
  const mutate = async (kind: "markRead" | "dismiss", ids: string[], all = false) => {
    if (get().busy || !inDesktop) return;
    if (useDaemon.getState().link === "offline") {
      set({ actionError: "The daemon is offline. Reconnect before changing notifications." });
      return;
    }
    set({ busy: true, actionError: null });
    try {
      await Notifications[kind](ids, get().source, all);
      await get().load();
    } catch (error) { set({ actionError: message(error) }); }
    finally { set({ busy: false }); }
  };
  return {
    items: [], sources: [], unread: 0, source: "", unreadOnly: false,
    loaded: false, loading: false, busy: false, error: null, actionError: null,
    prefs: null, prefsLoading: false, prefsSaving: false, prefsError: null, nativeError: null,
    nativeStatus: null, testing: false, testStatus: null, testError: null,
    async test() {
      if (!inDesktop || get().testing || get().prefsSaving || get().prefsLoading) return;
      if (useDaemon.getState().link === "offline") {
        set({ testError: "The daemon is offline. Reconnect before testing notifications.", testStatus: null });
        return;
      }
      set({ testing: true, testStatus: null, testError: null, nativeStatus: null, nativeError: null });
      try {
        const result = await Notifications.test();
        if (!result.stored) {
          set({ testStatus: "No notification created: system.test is disabled by your saved preferences." });
        } else {
          const delivery = result.native
            ? "Windows delivery requested; this does not confirm that a notification was displayed or heard."
            : "Windows notifications are disabled for system.test.";
          set({ testStatus: `Test saved to your notification center. ${delivery}${result.sound ? "" : " Notification sound is off."}` });
        }
      } catch (error) { set({ testError: message(error) }); }
      finally { set({ testing: false }); }
    },
    filter(source, unreadOnly) {
      set({ source, unreadOnly, items: [], loaded: false, actionError: null });
      void get().load();
    },
    async load() {
      if (!inDesktop) return;
      const request = ++listRequest;
      const { source, unreadOnly } = get();
      set({ loading: true });
      try {
        const result = await Notifications.list(source, unreadOnly);
        if (request === listRequest) set({ ...result, items: result.items ?? [], sources: result.sources ?? [], loaded: true, loading: false, error: null });
      } catch (error) {
        if (request === listRequest) set({ loading: false, error: message(error) });
      }
    },
    markRead: (ids, all) => mutate("markRead", ids, all),
    dismiss: (ids, all) => mutate("dismiss", ids, all),
    async loadPrefs() {
      if (!inDesktop || get().prefsSaving) return;
      const request = ++prefsRequest;
      set({ prefsLoading: true });
      try {
        const prefs = await Notifications.prefs();
        if (request === prefsRequest) set({ prefs: { ...prefs, sources: prefs.sources ?? {} }, prefsLoading: false, prefsError: null });
      } catch (error) {
        if (request === prefsRequest) set({ prefsLoading: false, prefsError: message(error) });
      }
    },
    async setSourcePref(source, pref) {
      const current = get().prefs;
      if (!current || get().prefsSaving || !inDesktop) return;
      if (useDaemon.getState().link === "offline") {
        set({ prefsError: "The daemon is offline. Reconnect before saving preferences." });
        return;
      }
      const next: Prefs = { default: current.default, sources: { ...current.sources } };
      if (source === null) {
        if (!pref) return;
        next.default = pref;
      } else if (pref) next.sources[source] = pref;
      else delete next.sources[source];
      ++prefsRequest;
      set({ prefsSaving: true, prefsLoading: false, prefsError: null });
      try {
        const prefs = await Notifications.setPrefs(next);
        set({ prefs: { ...prefs, sources: prefs.sources ?? {} } });
      } catch (error) { set({ prefsError: message(error) }); }
      finally { set({ prefsSaving: false }); }
    },
    startSync() {
      if (!inDesktop) return () => {};
      if (++subscribers === 1) {
        const refresh = () => { void get().load(); void get().loadPrefs(); };
        const offChanged = events.notificationsChanged.on(({ unread }) => {
          set({ unread });
          void get().load();
        });
        const offSettings = events.settingsChanged.on(({ key }) => {
          if (key === "notifications.prefs") void get().loadPrefs();
        });
        const offNative = events.notificationsNativeState.on(({ denied, error, accepted }) => {
          set({
            nativeError: denied ? (error || "Windows notifications are unavailable or blocked. Check system notification settings.") : (error ?? null),
            nativeStatus: accepted ? "Windows accepted the latest notification request. Display and sound still depend on Windows permissions, volume, and Do not disturb / Focus Assist." : null,
          });
        });
        const offDaemon = useDaemon.subscribe((state, previous) => {
          if (state.link === "connected" && previous.link !== "connected") refresh();
          if (state.link === "offline" && previous.link !== "offline") {
            ++listRequest;
            ++prefsRequest;
            set({ loading: false, prefsLoading: false, error: "The daemon is offline. Showing the last loaded notifications." });
          }
        });
        window.addEventListener("focus", refresh);
        refresh();
        stopSync = () => { offChanged(); offSettings(); offNative(); offDaemon(); window.removeEventListener("focus", refresh); };
      }
      let stopped = false;
      return () => {
        if (stopped) return;
        stopped = true;
        if (--subscribers === 0) { stopSync?.(); stopSync = undefined; }
      };
    },
  };
});
