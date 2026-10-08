import { Suspense, useEffect } from "react";
import { AppShell } from "@/components/shell/AppShell";
import { useApplyTheme } from "@/hooks/useApplyTheme";
import { registerCoreCommands } from "@/lib/coreCommands";
import { startDaemonPolling } from "@/stores/daemon";
import { useSettings } from "@/stores/settings";
import { useUI } from "@/stores/ui";
import { useWorkspaces } from "@/stores/workspaces";
import { NotImplemented } from "@/features/shared/States";
import { views } from "@/features/registry";
import { navItem } from "@/lib/nav";

function ViewHost() {
  const view = useUI((s) => s.view);
  const V = views[view];
  return V ? <V /> : <NotImplemented app={navItem(view).label} />;
}

export default function App() {
  useApplyTheme();
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
        <ViewHost />
      </Suspense>
    </AppShell>
  );
}
