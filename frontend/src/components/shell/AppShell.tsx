import { useEffect, type ReactNode } from "react";
import { PanelLeftOpen, Plus } from "lucide-react";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Sidebar } from "./Sidebar";
import { StatusBar } from "./StatusBar";
import { CommandPalette } from "./CommandPalette";
import { useUI } from "@/stores/ui";
import { inDesktop } from "@/lib/backend";
import { navItem } from "@/lib/nav";
import { Tip } from "@/components/tip";

export function AppShell({ children }: { children: ReactNode }) {
  const { setFocused, sidebarOpen, toggleSidebar, setPalette, view } = useUI();
  useEffect(() => {
    const f = () => setFocused(document.hasFocus() && !document.hidden);
    window.addEventListener("focus", f); window.addEventListener("blur", f); document.addEventListener("visibilitychange", f);
    f();
    return () => { window.removeEventListener("focus", f); window.removeEventListener("blur", f); document.removeEventListener("visibilitychange", f); };
  }, [setFocused]);
  useEffect(() => {
    const k = (e: KeyboardEvent) => { if (e.ctrlKey && e.key.toLowerCase() === "b") { e.preventDefault(); toggleSidebar(); } };
    window.addEventListener("keydown", k);
    return () => window.removeEventListener("keydown", k);
  }, [toggleSidebar]);
  const n = navItem(view);
  return (
    <TooltipProvider delay={300}>
      <div className="flex h-full flex-col bg-background">
        <div className="flex min-h-0 flex-1">
          {sidebarOpen && <Sidebar />}
          <div className="flex min-h-0 min-w-0 flex-1 flex-col">
            <div className="titlebar-drag flex h-8 shrink-0 items-center gap-2 border-b pl-2" style={{ height: "var(--titlebar-h)", paddingRight: "var(--mygo-titlebar-inset-right, 0.5rem)" }}>
              {!sidebarOpen && <Tip label="Show sidebar (Ctrl+B)" side="bottom"><button type="button" aria-label="Show sidebar" onClick={toggleSidebar} className="titlebar-nodrag inline-flex size-6 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground"><PanelLeftOpen className="size-3.5" /></button></Tip>}
              <button type="button" aria-label="Open command palette" onClick={() => setPalette(true)} className="titlebar-nodrag inline-flex size-6 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground"><Plus className="size-3.5" /></button>
              {view !== "home" && <h1 className="font-medium text-sm">{n.label}</h1>}
              {view !== "home" && n.blurb && <span className="truncate text-muted-foreground text-xs">{n.blurb}</span>}
            </div>
            {!inDesktop && (
              <div role="note" className="border-b bg-warning/10 px-3 py-1 text-warning-foreground text-xs">
                Browser preview: no daemon is connected. Only appearance preferences persist, in this browser.
              </div>
            )}
            <main id="main" tabIndex={-1} className="relative min-h-0 min-w-0 flex-1 overflow-hidden outline-none">{children}</main>
          </div>
        </div>
        <StatusBar />
      </div>
      <CommandPalette />
    </TooltipProvider>
  );
}
