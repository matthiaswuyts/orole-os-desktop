import { create } from "zustand";

export type ViewId = "home" | "agents" | "review" | "issues" | "automations" | "new-task" | "settings" | "chat";
/** The sidebar's two worlds. Settings is reachable from both and belongs to neither. */
export type Mode = "code" | "chat";

const MODE_KEY = "orole.mode";

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
  /** Last Code-mode view, restored when switching back from Chat. Chat mode always shows `chat`. */
  lastCodeView: Exclude<ViewId, "chat" | "settings">;
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
    setMode: (next) => {
      saveMode(next);
      set(next === "chat" ? { mode: next, view: "chat" } : { mode: next, view: get().lastCodeView });
    },
    settingsSection: "customization",
    settingsTab: "",
    openSettings: (settingsSection, settingsTab = "") => set({ settingsSection, settingsTab, view: "settings" }),
    paletteOpen: false,
    sidebarOpen: true,
    toggleSidebar: () => set((s) => ({ sidebarOpen: !s.sidebarOpen })),
    windowFocused: true,
    setView: (view) => {
      // Settings keeps whichever mode is active; every other view picks its own.
      const mode = view === "settings" ? get().mode : view === "chat" ? "chat" : "code";
      saveMode(mode);
      set((s) => ({ view, mode, lastCodeView: mode === "code" && view !== "settings" ? view as UIState["lastCodeView"] : s.lastCodeView }));
    },
    setPalette: (paletteOpen) => set({ paletteOpen }),
    setFocused: (windowFocused) => set({ windowFocused }),
  };
});

