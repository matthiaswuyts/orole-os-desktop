import { useEffect, type ReactNode } from "react";
import { PanelLeftOpen, Plus } from "lucide-react";
import { Tooltip, TooltipPopup, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { Sidebar } from "./Sidebar";
import { StatusBar } from "./StatusBar";
import { CommandPalette } from "./CommandPalette";
import { useUI } from "@/stores/ui";
import { inDesktop } from "@/lib/backend";
import { navItem } from "@/lib/nav";
import { Kbd, KbdGroup } from "@/components/ui/kbd";
import { NotificationCenter } from "@/features/notifications/NotificationCenter";

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
            <header className="titlebar-drag flex h-8 shrink-0 items-center gap-2 border-b pl-2" style={{ height: "var(--titlebar-h)", paddingRight: "var(--mygo-titlebar-inset-right, 0.5rem)" }}>
              {!sidebarOpen && <Tooltip><TooltipTrigger render={<button type="button" aria-label="Show sidebar" onClick={toggleSidebar} className="titlebar-nodrag inline-flex size-6 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground" />}><PanelLeftOpen className="size-3.5" /></TooltipTrigger><TooltipPopup side="bottom"><span className="flex items-center gap-2">Show sidebar <KbdGroup aria-label="Ctrl+B"><Kbd>Ctrl</Kbd><Kbd>B</Kbd></KbdGroup></span></TooltipPopup></Tooltip>}
              <Tooltip><TooltipTrigger render={<button type="button" aria-label="Open command palette" onClick={() => setPalette(true)} className="titlebar-nodrag inline-flex size-6 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground" />}><Plus className="size-3.5" /></TooltipTrigger><TooltipPopup side="bottom"><span className="flex items-center gap-2">Search <KbdGroup aria-label="Ctrl+K"><Kbd>Ctrl</Kbd><Kbd>K</Kbd></KbdGroup></span></TooltipPopup></Tooltip>
              {view !== "home" && <h1 className="font-medium text-sm">{n.label}</h1>}
              {view !== "home" && n.blurb && <span className="truncate text-muted-foreground text-xs">{n.blurb}</span>}
              <div className="titlebar-nodrag ml-auto flex h-full shrink-0 items-center gap-2 pl-2">
                <NotificationCenter />
                <span aria-hidden="true" className="mr-1 h-4 w-px bg-border" />
              </div>
            </header>
            {!inDesktop && (
              <aside aria-label="Browser preview status" className="border-b bg-warning/10 px-3 py-1 text-warning-foreground text-xs">
                Browser preview: no daemon is connected. Only appearance preferences persist, in this browser.
              </aside>
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
