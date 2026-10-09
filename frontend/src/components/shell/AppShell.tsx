import { useEffect, type ReactNode } from "react";
import { PanelLeftOpen } from "lucide-react";
import { Tooltip, TooltipPopup, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { Sidebar } from "./Sidebar";
import { StatusBar } from "./StatusBar";
import { CommandPalette } from "./CommandPalette";
import { useUI } from "@/stores/ui";
import { inDesktop } from "@/lib/backend";
import { navItem } from "@/lib/nav";
import { Kbd, KbdGroup } from "@/components/ui/kbd";
import { NotificationCenter } from "@/features/notifications/NotificationCenter";
import { useAgents } from "@/stores/agents";
import { useDaemon } from "@/stores/daemon";
import { TaskHeader } from "@/features/tasks/TaskHeader";
import { BotActions, BotTitle } from "@/features/chat/BotHeader";
import { useChat, useSelectedBot } from "@/stores/chat";

export function AppShell({ children }: { children: ReactNode }) {
  const { setFocused, sidebarOpen, toggleSidebar, view } = useUI();
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
  const task = useAgents((state) => view === "agents" && state.available && !state.offline && state.openId ? state.sessions[state.openId] : undefined);
  const link = useDaemon((state) => state.link);
  const activeTask = link === "offline" ? undefined : task;
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
              {activeTask ? <TaskHeader key={activeTask.id} session={activeTask} /> : bot ? <BotTitle key={bot.id} bot={bot} /> : <>
                {view !== "home" && view !== "new-task" && <h1 className="font-medium text-sm">{n.label}</h1>}
                {view !== "home" && view !== "new-task" && n.blurb && <span className="truncate text-muted-foreground text-xs">{n.blurb}</span>}
              </>}
              <div className="titlebar-nodrag ml-auto flex h-full shrink-0 items-center gap-2 pl-2">
                {bot && <BotActions bot={bot} />}
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
