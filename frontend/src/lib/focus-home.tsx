import { useEffect } from "react";

/** Returns keyboard focus to the app's main region when a popup that held it unmounts. */
export function rescueFocus(delay = 60) {
  setTimeout(() => {
    if (document.activeElement && document.activeElement !== document.body) return;
    document.getElementById("main")?.focus({ preventScroll: true });
  }, delay);
}

export function FocusRescue(): null {
  useEffect(() => () => rescueFocus(), []);
  return null;
}
