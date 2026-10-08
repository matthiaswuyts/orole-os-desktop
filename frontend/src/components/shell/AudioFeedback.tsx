import { useEffect } from "react";
import { events } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { disposeSounds, playSound, stopSounds } from "@/lib/audio";
import { useSettings } from "@/stores/settings";

/** Shared semantic activation feedback, including portaled controls and keyboard clicks. */
export function AudioFeedback() {
  useEffect(() => {
    const click = (event: MouseEvent) => {
      if (!event.isTrusted || event.defaultPrevented || !(event.target instanceof Element)) return;
      const control = event.target.closest("button,a[href],[role=button],[role=switch],[role=checkbox],[role=radio],[role=tab],[role=menuitem],[role=option]");
      if (!control || control.closest('[data-ui-sound="none"],[aria-disabled="true"],[inert]') || control.matches(":disabled")) return;
      const toggle = control.matches('[role=switch],[role=checkbox],[role=radio]');
      void playSound(toggle ? "toggle" : "activate");
    };
    const pause = () => { if (document.hidden || !document.hasFocus()) stopSounds(); };
    document.addEventListener("click", click);
    document.addEventListener("visibilitychange", pause);
    window.addEventListener("blur", pause);
    const offSettings = useSettings.subscribe((next, previous) => {
      if (next.sound.volume !== previous.sound.volume || next.sound.uiEnabled !== previous.sound.uiEnabled) stopSounds();
    });
    // History fetches/read/dismiss events never play. Windows owns native cues,
    // including suppressed/failed deliveries: no duplicate web fallback.
    const seen = new Set<string>();
    const offNotification = inDesktop ? events.notificationsChanged.on((event) => {
      const notification = event.notification;
      if (event.kind !== "created" || !event.sound || event.native || !notification || seen.has(notification.id)) return;
      seen.add(notification.id);
      if (seen.size > 128) seen.delete(seen.values().next().value!);
      const level = notification.level;
      void playSound(level === "success" || level === "warning" || level === "error" ? level : "info", "notification");
    }) : undefined;
    return () => {
      document.removeEventListener("click", click);
      document.removeEventListener("visibilitychange", pause);
      window.removeEventListener("blur", pause);
      offSettings();
      offNotification?.();
      disposeSounds();
    };
  }, []);
  return null;
}
