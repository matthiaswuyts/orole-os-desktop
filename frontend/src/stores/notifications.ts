import { create } from "zustand";
import { Notifications, events } from "@/mygo";
import type { NotificationList, Prefs, KindPref } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { onFocusRefresh } from "@/lib/focus";
import { useDaemon } from "@/stores/daemon";
import { targetOnScreen } from "@/features/notifications/targets";

export type NotificationItem = NotificationList["items"][number];
export type NotificationKind = NotificationItem["kind"];
export type NotificationTab = "all" | "needs-you";

const message = (error: unknown) => error instanceof Error ? error.message : String(error);
let listRequest = 0;
let prefsRequest = 0;
let subscribers = 0;
let stopSync: (() => void) | undefined;

interface State {
  items: NotificationItem[];
  unread: number;
  needsYou: number;
  tab: NotificationTab;
  loaded: boolean;
  loading: boolean;
  busy: boolean;
  error: string | null;
  actionError: string | null;
  prefs: Prefs | null;
  prefsLoading: boolean;
  prefsSaving: boolean;
  prefsError: string | null;
  setTab: (tab: NotificationTab) => void;
  load: () => Promise<void>;
  markRead: (ids: string[], all?: boolean) => Promise<void>;
  loadPrefs: () => Promise<void>;
  setKindPref: (kind: NotificationKind, pref: KindPref) => Promise<void>;
  startSync: () => () => void;
}

export const useNotifications = create<State>((set, get) => {
  const offline = () => useDaemon.getState().link === "offline";
  return {
    items: [], unread: 0, needsYou: 0, tab: "all",
    loaded: false, loading: false, busy: false, error: null, actionError: null,
    prefs: null, prefsLoading: false, prefsSaving: false, prefsError: null,
    setTab(tab) {
      if (tab === get().tab) return;
      set({ tab, items: [], loaded: false, actionError: null });
      void get().load();
    },
    async load() {
      if (!inDesktop) return;
      const request = ++listRequest;
      set({ loading: true });
      try {
        const result = await Notifications.list(get().tab === "needs-you");
        if (request === listRequest) set({ items: result.items ?? [], unread: result.unread, needsYou: result.needsYou, loaded: true, loading: false, error: null });
      } catch (error) {
        if (request === listRequest) set({ loading: false, error: message(error) });
      }
    },
    async markRead(ids, all = false) {
      if (get().busy || !inDesktop || (!all && ids.length === 0)) return;
      if (offline()) {
        set({ actionError: "The daemon is offline. Reconnect before changing notifications." });
        return;
      }
      // Shown read at once; the change event reloads the list and counts with the stored state.
      set((s) => {
        const marked = s.items.filter((item) => !item.read && (all || ids.includes(item.id))).length;
        return { busy: true, actionError: null, unread: all ? 0 : Math.max(0, s.unread - marked), items: s.items.map((item) => all || ids.includes(item.id) ? { ...item, read: true } : item) };
      });
      try {
        await Notifications.markRead(ids, all);
      } catch (error) {
        set({ actionError: message(error) });
        void get().load();
      } finally { set({ busy: false }); }
    },
    async loadPrefs() {
      if (!inDesktop || get().prefsSaving) return;
      const request = ++prefsRequest;
      set({ prefsLoading: true });
      try {
        const prefs = await Notifications.prefs();
        if (request === prefsRequest) set({ prefs, prefsLoading: false, prefsError: null });
      } catch (error) {
        if (request === prefsRequest) set({ prefsLoading: false, prefsError: message(error) });
      }
    },
    async setKindPref(kind, pref) {
      const current = get().prefs;
      if (!current || get().prefsSaving || !inDesktop) return;
      if (offline()) {
        set({ prefsError: "The daemon is offline. Reconnect before saving preferences." });
        return;
      }
      const next: Prefs = { kinds: { ...current.kinds, [kind]: pref } };
      ++prefsRequest;
      set({ prefs: next, prefsSaving: true, prefsLoading: false, prefsError: null });
      try {
        set({ prefs: await Notifications.setPrefs(next) });
      } catch (error) { set({ prefs: current, prefsError: message(error) }); }
      finally { set({ prefsSaving: false }); }
    },
    startSync() {
      if (!inDesktop) return () => {};
      if (++subscribers === 1) {
        const refresh = () => { void get().load(); void get().loadPrefs(); };
        const offChanged = events.notificationsChanged.on(({ kind, notification, unread, needsYou }) => {
          set({ unread, needsYou });
          // What the focused window already shows is seen as it arrives.
          if (kind === "created" && notification && !notification.read && targetOnScreen(notification.target)) void Notifications.markRead([notification.id], false).catch(() => {});
          void get().load();
        });
        const offSettings = events.settingsChanged.on(({ key }) => {
          if (key === "notifications.prefs") void get().loadPrefs();
        });
        const offDaemon = useDaemon.subscribe((state, previous) => {
          if (state.link === "connected" && previous.link !== "connected") refresh();
          if (state.link === "offline" && previous.link !== "offline") {
            ++listRequest;
            ++prefsRequest;
            set({ loading: false, prefsLoading: false, error: "The daemon is offline. Showing the last loaded notifications." });
          }
        });
        const offFocus = onFocusRefresh(refresh);
        refresh();
        stopSync = () => { offChanged(); offSettings(); offDaemon(); offFocus(); };
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
