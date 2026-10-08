import { create } from "zustand";

export type ViewId =
  | "home" | "mail" | "drive" | "github" | "agents" | "projects" | "terminals"
  | "automations" | "machines" | "applications" | "settings";

interface UIState {
  view: ViewId;
  paletteOpen: boolean;
  windowFocused: boolean;
  setView: (v: ViewId) => void;
  setPalette: (o: boolean) => void;
  setFocused: (f: boolean) => void;
}

export const useUI = create<UIState>((set) => ({
  view: "home",
  paletteOpen: false,
  windowFocused: true,
  setView: (view) => set({ view }),
  setPalette: (paletteOpen) => set({ paletteOpen }),
  setFocused: (windowFocused) => set({ windowFocused }),
}));
