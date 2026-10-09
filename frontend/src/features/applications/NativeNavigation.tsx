import { useEffect } from "react";
import { Desktop, events, type DesktopNavigation } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { NAV } from "@/lib/nav";
import { views } from "@/features/registry";
import { useUI } from "@/stores/ui";

/** Connect once at the shell root, not only when the launcher is open. */
export function connectNativeNavigation() {
  let active = true;
  let latest = 0;
  const navigate = (request: DesktopNavigation) => {
    if (!active || !Number.isSafeInteger(request.sequence) || request.sequence <= latest) return;
    if (request.view === "settings/skills" || request.view === "settings/mcp") {
      latest = request.sequence;
      useUI.getState().openSettings(request.view.slice("settings/".length));
      return;
    }
    const target = NAV.find((item) => item.id === request.view);
    if (!target || !views[target.id]) return;
    latest = request.sequence;
    useUI.getState().setView(target.id);
  };
  // Subscribe before fetching: an event can arrive while the cold-start snapshot
  // is in flight. Sequence prevents the older snapshot from overriding it.
  const off = events.desktopNavigate.on(navigate);
  void Desktop.ready().then(navigate, (error: unknown) => {
    if (active) console.warn("Native navigation handshake failed", error);
  });
  return () => { active = false; off(); };
}

export function NativeNavigation() {
  useEffect(() => {
    if (inDesktop) return connectNativeNavigation();
  }, []);
  return null;
}
