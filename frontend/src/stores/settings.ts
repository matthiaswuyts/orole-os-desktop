import { create } from "zustand";
import { Settings, events } from "@/mygo";
import { inDesktop, previewKey } from "@/lib/backend";
import type { BuiltinThemeId } from "@/themes/builtin";

export type ThemeId = BuiltinThemeId | "custom";
export type Density = "compact" | "comfortable";

export interface Appearance {
  theme: ThemeId;
  density: Density;
  /** Validated token overrides for theme "custom" (see themes/custom.ts). */
  customTokens: Record<string, string>;
}

export interface AsciiSettings {
  scene: string;
  enabled: boolean;
  paused: boolean;
  /** Frames per second ceiling (1-30). */
  fps: number;
  /** Layer opacity 0-1. */
  opacity: number;
  /** 0-1: scales fps and opacity together for quick "calm" control. */
  intensity: number;
  /** Throttle to a still frame when the window is unfocused/hidden. */
  pauseWhenUnfocused: boolean;
  /** Honour prefers-reduced-motion (default true). */
  respectReducedMotion: boolean;
  /** Force a still frame regardless of other settings. */
  staticOnly: boolean;
}

export interface Accessibility {
  reduceMotion: "system" | "on" | "off";
}

export interface TerminalSettings {
  fontSize: number;
  shell: string;
}

export interface SoundSettings {
  uiEnabled: boolean;
  /** Web Audio gain; native notification volume belongs to Windows. */
  volume: number;
}

function soundSettings(value: unknown): SoundSettings {
  const input = value && typeof value === "object" ? value as Partial<SoundSettings> : {};
  return {
    uiEnabled: typeof input.uiEnabled === "boolean" ? input.uiEnabled : true,
    volume: typeof input.volume === "number" && Number.isFinite(input.volume) ? Math.min(1, Math.max(0, input.volume)) : 0.35,
  };
}

export interface SettingsState {
  loaded: boolean;
  error: string | null;
  appearance: Appearance;
  ascii: AsciiSettings;
  accessibility: Accessibility;
  terminal: TerminalSettings;
  sound: SoundSettings;
  load: () => Promise<void>;
  set: <K extends "appearance" | "ascii" | "accessibility" | "terminal" | "sound">(key: K, patch: Partial<SettingsState[K]>) => void;
}

export const defaults = {
  appearance: { theme: "midnight", density: "compact", customTokens: {} } as Appearance,
  ascii: {
    scene: "night-coast", enabled: true, paused: false, fps: 12, opacity: 1, intensity: 1,
    pauseWhenUnfocused: true, respectReducedMotion: true, staticOnly: false,
  } as AsciiSettings,
  accessibility: { reduceMotion: "system" } as Accessibility,
  terminal: { fontSize: 13, shell: "" } as TerminalSettings,
  sound: soundSettings(undefined),
};

async function readAll(): Promise<Record<string, unknown>> {
  if (inDesktop) return Settings.all();
  try {
    return JSON.parse(localStorage.getItem(previewKey) ?? "{}") as Record<string, unknown>;
  } catch {
    return {};
  }
}

async function write(key: string, value: unknown): Promise<void> {
  if (inDesktop) return Settings.set(key, value);
  const all = await readAll();
  all[key] = value;
  localStorage.setItem(previewKey, JSON.stringify(all));
}

export const useSettings = create<SettingsState>((set, get) => ({
  loaded: false,
  error: null,
  ...structuredClone(defaults),
  async load() {
    try {
      const all = await readAll();
      set({
        loaded: true,
        error: null,
        appearance: { ...defaults.appearance, ...(all.appearance as object) },
        ascii: { ...defaults.ascii, ...(all.ascii as object) },
        accessibility: { ...defaults.accessibility, ...(all.accessibility as object) },
        terminal: { ...defaults.terminal, ...(all.terminal as object) },
        sound: soundSettings(all.sound),
      });
    } catch (e) {
      set({ loaded: true, error: e instanceof Error ? e.message : String(e) });
    }
  },
  set(key, patch) {
    const next = key === "sound" ? soundSettings({ ...get().sound, ...patch }) : { ...get()[key], ...patch };
    set({ [key]: next } as Partial<SettingsState>);
    write(key, next).catch((e) => set({ error: e instanceof Error ? e.message : String(e) }));
  },
}));

if (inDesktop) {
  // Settings changed through another window or the CLI.
  events.settingsChanged.on(({ key, value }) => {
    if (key === "sound") {
      useSettings.setState({ sound: soundSettings(value) });
      return;
    }
    if (key === "appearance" || key === "ascii" || key === "accessibility" || key === "terminal") {
      useSettings.setState({ [key]: { ...defaults[key], ...(value as object) } } as Partial<SettingsState>);
    }
  });
}
