import { create } from "zustand";

export type ViewId =
  | "home" | "agents" | "review" | "worktrees" | "automations" | "mail" | "drive" | "github"
  | "projects" | "terminals" | "machines" | "applications" | "browser" | "files" | "settings";

interface UIState {
  view: ViewId;
  settingsSection: string;
  openSettings: (section: string) => void;
  pendingMailThread: string | null;
  openMailThread: (id: string) => void;
  clearMailThread: () => void;
  pendingDriveFile: { id: string; name: string; isFolder: boolean } | null;
  openDriveFile: (file: { id: string; name: string; isFolder: boolean }) => void;
  clearDriveFile: () => void;
  pendingTerminal: string | null;
  openTerminal: (id: string) => void;
  clearTerminal: () => void;
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
  settingsSection: "appearance",
  openSettings: (settingsSection) => set({ settingsSection, view: "settings" }),
  pendingMailThread: null,
  openMailThread: (pendingMailThread) => set({ pendingMailThread, view: "mail" }),
  clearMailThread: () => set({ pendingMailThread: null }),
  pendingDriveFile: null,
  openDriveFile: (pendingDriveFile) => set({ pendingDriveFile, view: "drive" }),
  clearDriveFile: () => set({ pendingDriveFile: null }),
  pendingTerminal: null,
  openTerminal: (pendingTerminal) => set({ pendingTerminal, view: "terminals" }),
  clearTerminal: () => set({ pendingTerminal: null }),
  paletteOpen: false,
  sidebarOpen: true,
  toggleSidebar: () => set((s) => ({ sidebarOpen: !s.sidebarOpen })),
  windowFocused: true,
  setView: (view) => set({ view }),
  setPalette: (paletteOpen) => set({ paletteOpen }),
  setFocused: (windowFocused) => set({ windowFocused }),
}));
