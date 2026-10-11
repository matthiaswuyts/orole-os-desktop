import { Suspense, useEffect } from "react";
import { events } from "@/mygo";
import { useApplyTheme } from "@/hooks/useApplyTheme";
import { inDesktop } from "@/lib/backend";
import { startDaemonPolling } from "@/stores/daemon";
import { useSettings } from "@/stores/settings";
import { useUI } from "@/stores/ui";
import { useWorkspaces } from "@/stores/workspaces";
import { ToastProvider } from "@/components/ui/toast";
import { TooltipProvider } from "@/components/ui/tooltip";
import { SettingsView } from "./SettingsView";

/** Query flag the desktop shell puts on the URL of the isolated settings window. */
export const isSettingsWindow = new URLSearchParams(window.location.search).get("window") === "settings";

/** Root of the settings window: the settings sidebar and pages only, no app shell. */
export function SettingsWindow() {
  useApplyTheme();
  const loaded = useSettings((s) => s.loaded);
  useEffect(() => {
    const section = new URLSearchParams(window.location.search).get("section");
    if (section) useUI.getState().openSettings(section);
    void useSettings.getState().load();
    void useWorkspaces.getState().load();
    const offPolling = startDaemonPolling();
    const offSection = inDesktop ? events.settingsWindowSection.on((s) => useUI.getState().openSettings(s)) : undefined;
    return () => { offPolling(); offSection?.(); };
  }, []);
  return (
    <ToastProvider>
      <TooltipProvider delay={300}>
        <div className="flex h-full flex-col bg-background">
          <header className="titlebar-drag flex shrink-0 items-center border-b pl-3" style={{ height: "var(--titlebar-h)", paddingRight: "var(--mygo-titlebar-inset-right, 0.5rem)" }}>
            <span className="font-medium text-sm">Settings</span>
          </header>
          <main id="main" tabIndex={-1} className="relative min-h-0 flex-1 overflow-hidden outline-none">
            {loaded
              ? <Suspense fallback={<div role="status" className="p-6 text-muted-foreground">Loading…</div>}><SettingsView /></Suspense>
              : <div role="status" className="p-6 text-muted-foreground">Loading preferences…</div>}
          </main>
        </div>
      </TooltipProvider>
    </ToastProvider>
  );
}
