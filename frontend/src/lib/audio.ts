import { defineSound, dispose, ensureReady, type PlayOptions, type SoundDefinition, type VoiceHandle } from "@web-kits/audio";
import { create } from "zustand";
import { useSettings } from "@/stores/settings";

// One engine for component and in-app notification cues; no remote samples.
export const soundDefinitions = {
  activate: { source: { type: "sine", frequency: { start: 740, end: 520 } }, envelope: { attack: 0.003, decay: 0.045 }, gain: 0.12 },
  toggle: { source: { type: "sine", frequency: { start: 520, end: 780 } }, envelope: { attack: 0.003, decay: 0.065 }, gain: 0.14 },
  info: { layers: [
    { source: { type: "sine", frequency: 660 }, envelope: { attack: 0.006, decay: 0.16 }, gain: 0.17 },
    { source: { type: "sine", frequency: 880 }, delay: 0.09, envelope: { attack: 0.006, decay: 0.2 }, gain: 0.12 },
  ] },
  success: { layers: [
    { source: { type: "sine", frequency: 660 }, envelope: { attack: 0.006, decay: 0.13 }, gain: 0.16 },
    { source: { type: "sine", frequency: 990 }, delay: 0.08, envelope: { attack: 0.006, decay: 0.19 }, gain: 0.13 },
  ] },
  warning: { source: { type: "triangle", frequency: { start: 520, end: 440 } }, envelope: { attack: 0.006, decay: 0.23 }, gain: 0.12 },
  error: { layers: [
    { source: { type: "sine", frequency: 440 }, envelope: { attack: 0.006, decay: 0.14 }, gain: 0.16 },
    { source: { type: "sine", frequency: 330 }, delay: 0.09, envelope: { attack: 0.006, decay: 0.21 }, gain: 0.13 },
  ] },
} satisfies Record<string, SoundDefinition>;
export type SoundCue = keyof typeof soundDefinitions;
type Channel = "ui" | "notification";
const players = Object.fromEntries(Object.entries(soundDefinitions).map(([key, definition]) => [key, defineSound(definition)])) as Record<SoundCue, (options?: PlayOptions) => VoiceHandle>;
const voices: Partial<Record<Channel, VoiceHandle>> = {};
const lastStarted: Record<Channel, number> = { ui: -Infinity, notification: -Infinity };
let context: AudioContext | undefined;
let epoch = 0;
export const useAudioStatus = create<{ error: string | null }>(() => ({ error: null }));

function permitted(channel: Channel) {
  const { loaded, sound } = useSettings.getState();
  return loaded && sound.volume > 0 && (channel !== "ui" || sound.uiEnabled)
    && !document.hidden && document.hasFocus() && navigator.userActivation?.hasBeenActive;
}

/** Never queue stale feedback behind autoplay restrictions or a window change. */
export async function playSound(cue: SoundCue, channel: Channel = "ui"): Promise<boolean> {
  if (!permitted(channel)) return false;
  const at = performance.now();
  if (at - lastStarted[channel] < (channel === "ui" ? 60 : 700)) return false;
  lastStarted[channel] = at;
  const generation = epoch;
  try {
    const ready = await ensureReady();
    if (generation !== epoch) return false;
    context = ready;
    if (ready.state !== "running" || !permitted(channel) || performance.now() - at > 250) return false;
    voices[channel]?.stop();
    voices[channel] = players[cue]({ volume: useSettings.getState().sound.volume });
    useAudioStatus.setState({ error: null });
    return true;
  } catch (error) {
    if (generation === epoch) useAudioStatus.setState({ error: `Sound unavailable: ${error instanceof Error ? error.message : String(error)}` });
    return false;
  }
}

export function stopSounds() {
  ++epoch;
  voices.ui?.stop();
  voices.notification?.stop();
  delete voices.ui;
  delete voices.notification;
  if (context?.state === "running") void context.suspend().catch((error: unknown) => {
    useAudioStatus.setState({ error: `Could not suspend sound: ${error instanceof Error ? error.message : String(error)}` });
  });
}

export function disposeSounds() {
  stopSounds();
  dispose();
  context = undefined;
}
