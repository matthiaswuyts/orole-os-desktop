import { Suspense, lazy, useEffect } from "react";
import { AppShell } from "@/components/shell/AppShell";
import { useApplyTheme } from "@/hooks/useApplyTheme";
import { registerCoreCommands } from "@/lib/coreCommands";
import { startDaemonPolling } from "@/stores/daemon";
import { useSettings } from "@/stores/settings";
import { useUI } from "@/stores/ui";
import { useWorkspaces } from "@/stores/workspaces";
import { HomeView } from "@/features/home/HomeView";
import { NotImplemented } from "@/features/shared/States";
import { NAV } from "@/lib/nav";

const TerminalsView = lazy(() => import("@/features/terminals/TerminalsView").then((m) => ({ default: m.TerminalsView })));
const SettingsView = lazy(() => import("@/features/settings/SettingsView").then((m) => ({ default: m.SettingsView })));

export default function App() {
  useApplyTheme();
  const view = useUI((s) => s.view);
  useEffect(() => {
    void useSettings.getState().load();
    void useWorkspaces.getState().load();
    const off1 = startDaemonPolling();
    const off2 = registerCoreCommands();
    return () => { off1(); off2(); };
  }, []);
  return (
    <AppShell>
      <Suspense fallback={<div role="status" className="p-6 text-muted-foreground">Loading…</div>}>
        {view === "home" && <HomeView />}
        {view === "terminals" && <TerminalsView />}
        {view === "settings" && <SettingsView />}
        {!["home", "terminals", "settings"].includes(view) && <NotImplemented app={NAV.find((n) => n.id === view)!.label} />}
      </Suspense>
    </AppShell>
  );
}
