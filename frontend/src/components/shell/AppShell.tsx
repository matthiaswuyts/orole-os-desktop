import { useEffect, type ReactNode } from "react";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Sidebar } from "./Sidebar";
import { StatusBar } from "./StatusBar";
import { TabBar } from "./TabBar";
import { CommandPalette } from "./CommandPalette";
import { useUI } from "@/stores/ui";
import { inDesktop } from "@/lib/backend";

export function AppShell({ children }: { children: ReactNode }) {
  const setFocused = useUI((s) => s.setFocused);
  useEffect(() => {
    const f = () => setFocused(document.hasFocus() && !document.hidden);
    window.addEventListener("focus", f); window.addEventListener("blur", f); document.addEventListener("visibilitychange", f);
    f();
    return () => { window.removeEventListener("focus", f); window.removeEventListener("blur", f); document.removeEventListener("visibilitychange", f); };
  }, [setFocused]);
  return (
    <TooltipProvider delay={300}>
      <div className="flex h-full flex-col bg-background">
        <TabBar />
        {!inDesktop && (
          <div role="note" className="border-b bg-warning/10 px-3 py-1 text-warning-foreground text-xs">
            Browser preview: no daemon is connected. Only appearance preferences persist, in this browser.
          </div>
        )}
        <div className="flex min-h-0 flex-1">
          <Sidebar />
          <main id="main" tabIndex={-1} className="relative min-h-0 min-w-0 flex-1 overflow-hidden outline-none">{children}</main>
        </div>
        <StatusBar />
      </div>
      <CommandPalette />
    </TooltipProvider>
  );
}
