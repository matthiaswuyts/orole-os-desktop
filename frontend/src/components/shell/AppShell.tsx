import { lazy, Suspense, useEffect, useState, type ReactNode } from "react";
import { PanelLeftOpen } from "lucide-react";
import { Tooltip, TooltipPopup, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { Sidebar } from "./Sidebar";
import { StatusBar } from "./StatusBar";
import { useUI } from "@/stores/ui";
import { inDesktop } from "@/lib/backend";
import { navItem } from "@/lib/nav";
import { Kbd, KbdGroup } from "@/components/ui/kbd";
import { NotificationCenter } from "@/features/notifications/NotificationCenter";
import { useAgents } from "@/stores/agents";
import { useDaemon } from "@/stores/daemon";
import { TaskHeader } from "@/features/tasks/TaskHeader";
import { sessionTitle } from "@/features/agents/format";
import { BotActions, BotTitle } from "@/features/chat/BotHeader";
import { useChat, useSelectedBot } from "@/stores/chat";

const loadPalette = () => import("./CommandPalette").then((m) => ({ default: m.CommandPalette }));
const CommandPalette = lazy(loadPalette);

export function AppShell({ children }: { children: ReactNode }) {
  const setFocused = useUI((s) => s.setFocused);
  const sidebarOpen = useUI((s) => s.sidebarOpen);
  const toggleSidebar = useUI((s) => s.toggleSidebar);
  const view = useUI((s) => s.view);
  const paletteOpen = useUI((s) => s.paletteOpen);
  const setPalette = useUI((s) => s.setPalette);
  // The palette lives in its own chunk: mounted on first open, then kept so the close animation and state survive.
  const [paletteMounted, setPaletteMounted] = useState(false);
  if (paletteOpen && !paletteMounted) setPaletteMounted(true);
  useEffect(() => {
    const t = setTimeout(() => void loadPalette(), 1500);
    return () => clearTimeout(t);
  }, []);
  useEffect(() => {
    const f = () => setFocused(document.hasFocus() && !document.hidden);
    window.addEventListener("focus", f); window.addEventListener("blur", f); document.addEventListener("visibilitychange", f);
    f();
    return () => { window.removeEventListener("focus", f); window.removeEventListener("blur", f); document.removeEventListener("visibilitychange", f); };
  }, [setFocused]);
  useEffect(() => {
    const k = (e: KeyboardEvent) => { if (e.ctrlKey && e.key.toLowerCase() === "b") { e.preventDefault(); toggleSidebar(); }
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") { e.preventDefault(); setPalette(!useUI.getState().paletteOpen); }
    };
    window.addEventListener("keydown", k);
    return () => window.removeEventListener("keydown", k);
  }, [toggleSidebar, setPalette]);
  const n = navItem(view);
  const task = useAgents((state) => view === "agents" && state.available && !state.offline && state.openId ? state.sessions[state.openId] : undefined);
  const link = useDaemon((state) => state.link);
  const activeTask = link === "offline" ? undefined : task;
  // A task that was just sent has no session yet; its page and titlebar come from the pending launch.
  const launch = useAgents((state) => view === "agents" && state.openId ? state.launches[state.openId] : undefined);
  const chatBot = useSelectedBot();
  const builderOpen = useChat((state) => state.builder.open);
  const bot = view === "chat" && !builderOpen ? chatBot : undefined;
  return (
    <TooltipProvider delay={300}>
      <div className="flex h-full flex-col bg-background">
        <div className="flex min-h-0 flex-1">
          {sidebarOpen && <Sidebar />}
          <div className="flex min-h-0 min-w-0 flex-1 flex-col">
            <header className="titlebar-drag flex h-8 shrink-0 items-center gap-2 border-b pl-2" style={{ height: "var(--titlebar-h)", paddingRight: "var(--mygo-titlebar-inset-right, 0.5rem)" }}>
              {!sidebarOpen && <Tooltip><TooltipTrigger render={<button type="button" aria-label="Show sidebar" onClick={toggleSidebar} className="titlebar-nodrag inline-flex size-6 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground" />}><PanelLeftOpen className="size-3.5" /></TooltipTrigger><TooltipPopup side="bottom"><span className="flex items-center gap-2">Show sidebar <KbdGroup aria-label="Ctrl+B"><Kbd>Ctrl</Kbd><Kbd>B</Kbd></KbdGroup></span></TooltipPopup></Tooltip>}
              {activeTask ? <TaskHeader key={activeTask.id} kind={activeTask.kind} title={sessionTitle(activeTask)} /> : launch ? <TaskHeader key={launch.id} kind={launch.kind} title={launch.title} /> : bot ? <BotTitle key={bot.id} bot={bot} /> : <>
                {view !== "home" && view !== "new-task" && <h1 className="font-medium text-sm">{n.label}</h1>}
                {view !== "home" && view !== "new-task" && n.blurb && <span className="truncate text-muted-foreground text-xs">{n.blurb}</span>}
              </>}
              <div className="titlebar-nodrag ml-auto flex h-full shrink-0 items-center gap-2 pl-2">
                {bot && <BotActions />}
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
      {paletteMounted && <Suspense fallback={null}><CommandPalette /></Suspense>}
    </TooltipProvider>
  );
}
