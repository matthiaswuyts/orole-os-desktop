import { defineSound, dispose, ensureReady, type PlayOptions, type SoundDefinition, type VoiceHandle } from "@web-kits/audio";
import { create } from "zustand";
import { useSettings } from "@/stores/settings";

// One engine for component and notification cues; no remote samples. Each notification kind has its own cue,
// so what happened is audible without looking: approval asks, failed falls, finished resolves upward,
// reply is a soft pop, message a single chime.
export const soundDefinitions = {
  activate: { source: { type: "sine", frequency: { start: 740, end: 520 } }, envelope: { attack: 0.003, decay: 0.045 }, gain: 0.12 },
  toggle: { source: { type: "sine", frequency: { start: 520, end: 780 } }, envelope: { attack: 0.003, decay: 0.065 }, gain: 0.14 },
  approval: { layers: [
    { source: { type: "triangle", frequency: 784 }, envelope: { attack: 0.005, decay: 0.12 }, gain: 0.16 },
    { source: { type: "triangle", frequency: 1047 }, delay: 0.12, envelope: { attack: 0.005, decay: 0.12 }, gain: 0.15 },
    { source: { type: "triangle", frequency: 1047 }, delay: 0.26, envelope: { attack: 0.005, decay: 0.2 }, gain: 0.12 },
  ] },
  failed: { layers: [
    { source: { type: "sine", frequency: 523 }, envelope: { attack: 0.006, decay: 0.14 }, gain: 0.16 },
    { source: { type: "sine", frequency: 415 }, delay: 0.1, envelope: { attack: 0.006, decay: 0.16 }, gain: 0.15 },
    { source: { type: "triangle", frequency: { start: 330, end: 300 } }, delay: 0.2, envelope: { attack: 0.006, decay: 0.28 }, gain: 0.12 },
  ] },
  finished: { layers: [
    { source: { type: "sine", frequency: 659 }, envelope: { attack: 0.005, decay: 0.12 }, gain: 0.14 },
    { source: { type: "sine", frequency: 831 }, delay: 0.07, envelope: { attack: 0.005, decay: 0.14 }, gain: 0.13 },
    { source: { type: "sine", frequency: 988 }, delay: 0.14, envelope: { attack: 0.005, decay: 0.26 }, gain: 0.13 },
  ] },
  // A chat "bubble": a tiny droplet pop, then a bright upward chirp (B5 → E6) in a small room. The only cue with a
  // noise transient and reverb, so a reply is recognisable from the generic chimes.
  reply: { layers: [
    { source: { type: "noise", color: "white" }, filter: { type: "bandpass", frequency: 3200, resonance: 4 }, envelope: { attack: 0.001, decay: 0.025 }, gain: 0.05 },
    { source: { type: "sine", frequency: { start: 1500, end: 650 } }, envelope: { attack: 0.002, decay: 0.05 }, gain: 0.11 },
    { source: { type: "sine", frequency: 988 }, delay: 0.07, envelope: { attack: 0.004, decay: 0.08 }, gain: 0.12 },
    { source: { type: "sine", frequency: { start: 1250, end: 1319 } }, delay: 0.14, envelope: { attack: 0.004, decay: 0.22 }, gain: 0.11 },
  ], effects: [{ type: "reverb", decay: 0.6, damping: 0.5, mix: 0.18 }] },
  message: { layers: [
    { source: { type: "sine", frequency: 880 }, envelope: { attack: 0.006, decay: 0.32 }, gain: 0.14 },
    { source: { type: "sine", frequency: 1760 }, envelope: { attack: 0.006, decay: 0.16 }, gain: 0.04 },
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

/** Interface cues play only in the focused window; notification cues also play in the background, since there is no other alert. */
function permitted(channel: Channel) {
  const { loaded, sound } = useSettings.getState();
  if (!loaded || sound.volume <= 0 || !navigator.userActivation?.hasBeenActive) return false;
  return channel === "notification" || (sound.uiEnabled && !document.hidden && document.hasFocus());
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
