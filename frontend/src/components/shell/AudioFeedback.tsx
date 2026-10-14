import { useEffect } from "react";
import { events } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { disposeSounds, playSound, stopSounds } from "@/lib/audio";
import { useSettings } from "@/stores/settings";
import { targetOnScreen } from "@/features/notifications/targets";

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
    // Only new notifications play, each kind with its own cue; history loads and read changes never do.
    // Nothing plays for what the focused window already shows.
    const seen = new Set<string>();
    const offNotification = inDesktop ? events.notificationsChanged.on((event) => {
      const notification = event.notification;
      if (event.kind !== "created" || !event.sound || !notification || seen.has(notification.id)) return;
      seen.add(notification.id);
      if (seen.size > 128) seen.delete(seen.values().next().value!);
      if (targetOnScreen(notification.target)) return;
      void playSound(notification.kind, "notification");
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
