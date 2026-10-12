import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { useSettings } from "@/stores/settings";
import { useDashboard } from "@/stores/dashboard";
import { useDaemon } from "@/stores/daemon";

/**
 * One startup gate; later refreshes and disconnects belong to the app, not this screen. Tasks are not a
 * check: `Agents.list` returns every session, and each view that shows them has its own loading state.
 */
export function StartupScreen({ children }: { children: ReactNode }) {
  const settingsLoaded = useSettings((state) => state.loaded);
  const dashboardLoaded = useDashboard((state) => state.loaded);
  const daemonLink = useDaemon((state) => state.link);
  const [opened, setOpened] = useState(false);
  const checks = [
    { done: settingsLoaded, label: "Loading preferences" },
    { done: dashboardLoaded, label: "Loading dashboard" },
    { done: daemonLink !== "unknown", label: "Checking daemon connection" },
  ];
  // These stores settle their initial checks on both success and failure. Completion
  // measures checks performed, not elapsed time or a claim that the daemon is online.
  const completed = checks.filter((check) => check.done).length;
  const complete = completed === checks.length;
  const percent = Math.round(completed / checks.length * 100);
  const status = checks.find((check) => !check.done)?.label ?? "Opening Orole OS";
  const filled = Math.floor(completed / checks.length * 26);
  // The classic ASCII style from ascii.rest/progress-bar, without its simulated loops.
  const bar = Array.from({ length: 26 }, (_, index) => index < filled ? "=" : index === filled ? ">" : " ").join("");

  useEffect(() => {
    if (!complete || opened) return;
    // Paint the real 100% state before handing off. No artificial minimum load time.
    let handoff = 0;
    const paint = requestAnimationFrame(() => {
      handoff = requestAnimationFrame(() => setOpened(true));
    });
    return () => { cancelAnimationFrame(paint); cancelAnimationFrame(handoff); };
  }, [complete, opened]);

  if (opened) return children;
  return (
    <main aria-label="Starting Orole OS" className="relative grid h-full min-h-0 place-items-center bg-background px-4 text-foreground">
      <div aria-hidden="true" className="titlebar-drag absolute inset-x-0 top-0" style={{ height: "var(--titlebar-h)" }} />
      <div className="flex max-w-full flex-col items-center gap-3">
        <h1 className="text-sm font-medium">Orole OS</h1>
        <div role="progressbar" aria-label="Startup checks" aria-valuemin={0} aria-valuemax={100}
          aria-valuenow={percent} aria-valuetext={`${completed} of ${checks.length} startup checks complete`}>
          <pre aria-hidden="true" className="m-0 whitespace-pre font-mono text-xs leading-6 sm:text-sm">{`[${bar}] ${String(percent).padStart(3)}%`}</pre>
        </div>
        <p role="status" className="text-center text-xs text-muted-foreground">{status}…</p>
      </div>
    </main>
  );
}
