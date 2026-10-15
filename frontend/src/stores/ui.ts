import { create } from "zustand";
import { Desktop } from "@/mygo";
import { inDesktop } from "@/lib/backend";

export type ViewId = "home" | "agents" | "review" | "issues" | "automations" | "new-task" | "settings" | "chat";

const isSettingsWindow = () => new URLSearchParams(window.location.search).get("window") === "settings";

const RAIL_KEY = "orole.taskRail";
const readRail = () => {
  try { return localStorage.getItem(RAIL_KEY) !== "closed"; } catch { return true; }
};
const writeRail = (open: boolean) => {
  try { localStorage.setItem(RAIL_KEY, open ? "open" : "closed"); } catch {
    // The choice then lasts for the session only.
  }
};

interface UIState {
  view: ViewId;
  /** Saved automation the Automations view should open, set by the palette; the view clears it once handled. */
  automationFocus: string | null;
  /** Opens the Automations view focused on one flow (`"new"` starts a new one); `null` clears the handoff. */
  focusAutomation: (id: string | null) => void;
  settingsSection: string;
  /** Tab (or in-page anchor) inside the settings section; "" = the section's first. */
  settingsTab: string;
  openSettings: (section: string, tab?: string) => void;
  paletteOpen: boolean;
  sidebarOpen: boolean;
  toggleSidebar: () => void;
  /** The details rail beside an open code task (environment, context, connectors, changes, background tasks). Remembered per browser profile. */
  taskRailOpen: boolean;
  toggleTaskRail: () => void;
  windowFocused: boolean;
  setView: (v: ViewId) => void;
  setPalette: (o: boolean) => void;
  setFocused: (f: boolean) => void;
}

export const useUI = create<UIState>((set, get) => ({
  view: "home",
  automationFocus: null,
  focusAutomation: (automationFocus) => {
    set({ automationFocus });
    if (automationFocus !== null) get().setView("automations");
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
  taskRailOpen: readRail(),
  toggleTaskRail: () => set((s) => { writeRail(!s.taskRailOpen); return { taskRailOpen: !s.taskRailOpen }; }),
  windowFocused: true,
  setView: (view) => set({ view }),
  setPalette: (paletteOpen) => set({ paletteOpen }),
  setFocused: (windowFocused) => set({ windowFocused }),
}));
