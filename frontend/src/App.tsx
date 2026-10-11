import { lazy, Suspense, useEffect } from "react";
import { AppShell } from "@/components/shell/AppShell";
import { useApplyTheme } from "@/hooks/useApplyTheme";
import { registerCoreCommands } from "@/lib/coreCommands";
import { startDaemonPolling } from "@/stores/daemon";
import { useSettings } from "@/stores/settings";
import { useUI } from "@/stores/ui";
import { useWorkspaces } from "@/stores/workspaces";
import { useAgents } from "@/stores/agents";
import { NotImplemented } from "@/features/shared/States";
import { views } from "@/features/registry";
import { navItem } from "@/lib/nav";
import { PolicyApprovals } from "@/features/mcp/PolicyApprovals";
import { NativeNavigation } from "@/features/applications/NativeNavigation";
import { PluginContributions } from "@/features/plugins";
import { AudioFeedback } from "@/components/shell/AudioFeedback";
import { useUpdates } from "@/stores/updates";
import { ToastProvider } from "@/components/ui/toast";
import { NewProjectDialog } from "@/features/projects/dialogs";
import { StartupScreen } from "@/components/shell/StartupScreen";

/** Query flag the desktop shell puts on the URL of the isolated settings window. */
const isSettingsWindow = new URLSearchParams(window.location.search).get("window") === "settings";
// The settings window is its own webview; the main window never loads its pages.
const SettingsWindow = lazy(() => import("@/features/settings/SettingsWindow").then((m) => ({ default: m.SettingsWindow })));

function ViewHost() {
  const view = useUI((s) => s.view);
  const V = views[view];
  return V ? <V /> : <NotImplemented app={navItem(view).label} />;
}

function MainApp() {
  useApplyTheme();
  useEffect(() => {
    void useSettings.getState().load();
    void useWorkspaces.getState().load();
    const off1 = startDaemonPolling();
    const off2 = registerCoreCommands();
    const offAgents = useAgents.getState().init();
    const offUpdates = useUpdates.getState().startSync();
    return () => { off1(); off2(); offAgents(); offUpdates(); };
  }, []);
  return (
    <ToastProvider>
      <NativeNavigation />
      <StartupScreen>
      <AppShell>
        <Suspense fallback={<div role="status" className="p-6 text-muted-foreground">Loading…</div>}>
          <ViewHost />
        </Suspense>
        <PolicyApprovals />
        <PluginContributions />
        <AudioFeedback />
        <NewProjectDialog />
      </AppShell>
      </StartupScreen>
    </ToastProvider>
  );
}

export default function App() {
  return isSettingsWindow ? <Suspense fallback={null}><SettingsWindow /></Suspense> : <MainApp />;
}
