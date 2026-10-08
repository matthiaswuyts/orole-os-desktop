import { create } from "zustand";

export type ViewId = "home" | "agents" | "review" | "automations" | "skills" | "mcp" | "settings";

interface UIState {
  view: ViewId;
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

export const useUI = create<UIState>((set) => ({
  view: "home",
  settingsSection: "customization",
  settingsTab: "",
  openSettings: (settingsSection, settingsTab = "") => set({ settingsSection, settingsTab, view: "settings" }),
  paletteOpen: false,
  sidebarOpen: true,
  toggleSidebar: () => set((s) => ({ sidebarOpen: !s.sidebarOpen })),
  windowFocused: true,
  setView: (view) => set({ view }),
  setPalette: (paletteOpen) => set({ paletteOpen }),
  setFocused: (windowFocused) => set({ windowFocused }),
}));
