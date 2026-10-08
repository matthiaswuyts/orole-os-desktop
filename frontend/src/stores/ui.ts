import { create } from "zustand";

export type ViewId =
  | "home" | "agents" | "review" | "worktrees" | "automations" | "mail" | "drive" | "github"
  | "projects" | "terminals" | "machines" | "applications" | "settings";

interface UIState {
  view: ViewId;
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
  paletteOpen: false,
  sidebarOpen: true,
  toggleSidebar: () => set((s) => ({ sidebarOpen: !s.sidebarOpen })),
  windowFocused: true,
  setView: (view) => set({ view }),
  setPalette: (paletteOpen) => set({ paletteOpen }),
  setFocused: (windowFocused) => set({ windowFocused }),
}));
