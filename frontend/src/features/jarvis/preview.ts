import { useSyncExternalStore } from "react";

/**
 * Voice previews: one shared audio element, so starting a preview stops any other. The URLs are ElevenLabs' public
 * preview clips (no key involved).
 */
let audio: HTMLAudioElement | null = null;
let playing: string | null = null;
const listeners = new Set<() => void>();

function emit() {
  for (const listener of listeners) listener();
}

function setPlaying(url: string | null) {
  if (playing === url) return;
  playing = url;
  emit();
}

export function stopPreview() {
  if (audio) {
    audio.pause();
    audio.removeAttribute("src");
    audio.load();
  }
  setPlaying(null);
}

/** Plays `url`, or stops it when it is already playing. */
export function togglePreview(url: string) {
  if (playing === url) {
    stopPreview();
    return;
  }
  stopPreview();
  if (!audio) {
    audio = new Audio();
    audio.addEventListener("ended", () => setPlaying(null));
    audio.addEventListener("error", () => setPlaying(null));
  }
  audio.src = url;
  setPlaying(url);
  audio.play().catch(() => { if (playing === url) setPlaying(null); });
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

/** Whether `url` is the preview playing now. */
export function usePreviewPlaying(url: string | undefined): boolean {
  return useSyncExternalStore(subscribe, () => !!url && playing === url, () => false);
}
