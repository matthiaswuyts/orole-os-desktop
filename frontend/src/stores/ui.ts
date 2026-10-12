import { create } from "zustand";
import { Desktop } from "@/mygo";
import { inDesktop } from "@/lib/backend";

export type ViewId = "home" | "agents" | "review" | "issues" | "automations" | "new-task" | "settings" | "chat";
/** Views that belong to the Bots mode (the `chat` mode id predates the "Bots" label). */
export type BotsView = "chat" | "automations";
const isBotsView = (view: ViewId): view is BotsView => view === "chat" || view === "automations";
/** The sidebar's two worlds. Settings is reachable from both and belongs to neither. */
export type Mode = "code" | "chat";

const MODE_KEY = "orole.mode";

const isSettingsWindow = () => new URLSearchParams(window.location.search).get("window") === "settings";

function loadMode(): Mode {
  try {
    return localStorage.getItem(MODE_KEY) === "chat" ? "chat" : "code";
  } catch {
    return "code";
  }
}

function saveMode(mode: Mode) {
  try {
    localStorage.setItem(MODE_KEY, mode);
  } catch {
    // Storage can be unavailable (private mode); the mode then lasts for the session only.
  }
}

interface UIState {
  view: ViewId;
  mode: Mode;
  /** Last Code-mode view, restored when switching back from Bots. */
  lastCodeView: Exclude<ViewId, BotsView | "settings">;
  /** Last Bots-mode view (the conversation or Automations), restored when switching back from Code. */
  lastBotsView: BotsView;
  /** Saved automation the Automations view should open, set by the palette; the view clears it once handled. */
  automationFocus: string | null;
  /** Opens the Automations view focused on one flow (`"new"` starts a new one); `null` clears the handoff. */
  focusAutomation: (id: string | null) => void;
  setMode: (m: Mode) => void;
  settingsSection: string;
  /** Tab (or in-page anchor) inside the settings section; "" = the section's first. */
  settingsTab: string;
  openSettings: (section: string, tab?: string) => void;
  paletteOpen: boolean;
  sidebarOpen: boolean;
  toggleSidebar: () => void;
  windowFocused: boolean;
  setView: (v: ViewId) => void;
  setPalette: (o: boolean) => void;
  setFocused: (f: boolean) => void;
}

export const useUI = create<UIState>((set, get) => {
  const mode = loadMode();
  return {
    view: mode === "chat" ? "chat" : "home",
    mode,
    lastCodeView: "home",
    lastBotsView: "chat",
    automationFocus: null,
    focusAutomation: (automationFocus) => {
      set({ automationFocus });
      if (automationFocus !== null) get().setView("automations");
    },
    setMode: (next) => {
      saveMode(next);
      set({ mode: next, view: next === "chat" ? get().lastBotsView : get().lastCodeView });
    },
    settingsSection: "customization",
    settingsTab: "",
    // The desktop app keeps settings in a window of their own; the settings window itself and the browser preview show them in place.
    openSettings: (settingsSection, settingsTab = "") => {
      set({ settingsSection, settingsTab });
      if (inDesktop && !isSettingsWindow()) {
        Desktop.openSettings(settingsSection).catch((error: unknown) => console.warn("Could not open the settings window", error));
        return;
      }
      set({ view: "settings" });
    },
    paletteOpen: false,
    sidebarOpen: true,
    toggleSidebar: () => set((s) => ({ sidebarOpen: !s.sidebarOpen })),
    windowFocused: true,
    setView: (view) => {
      // Settings keeps whichever mode is active; every other view picks its own.
      const bots = isBotsView(view);
      const mode = view === "settings" ? get().mode : bots ? "chat" : "code";
      saveMode(mode);
      set((s) => ({
        view, mode,
        lastBotsView: bots ? view : s.lastBotsView,
        lastCodeView: !bots && view !== "settings" ? view : s.lastCodeView,
      }));
    },
    setPalette: (paletteOpen) => set({ paletteOpen }),
    setFocused: (windowFocused) => set({ windowFocused }),
  };
});

