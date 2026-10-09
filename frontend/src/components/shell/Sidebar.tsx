import { ChevronDown, Code, MessagesSquare, PanelLeftClose, Search } from "lucide-react";
import { PRIMARY, SETTINGS, type NavItem } from "@/lib/nav";
import { cn } from "@/lib/utils";
import { useUI, type Mode } from "@/stores/ui";
import { useWorkspaces } from "@/stores/workspaces";
import { Kbd, KbdGroup } from "@/components/ui/kbd";
import { Menu, MenuItem, MenuPopup, MenuSeparator, MenuTrigger } from "@/components/ui/menu";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { SidebarProjects } from "./SidebarProjects";
import { Logo } from "./Logo";
import { useAgentCounts } from "@/stores/agents";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsList, TabsTab } from "@/components/ui/tabs";
import { BotRail } from "./BotRail";

const row = "flex h-side-row w-full items-center gap-2 rounded-md px-2 text-left text-sidebar-foreground text-sm outline-none hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring";

const MODES: { id: Mode; label: string; icon: typeof Code }[] = [
  { id: "code", label: "Code", icon: Code },
  { id: "chat", label: "Chat", icon: MessagesSquare },
];

/** Workspace controls, navigation, projects, and settings. */
export function Sidebar() {
  const { view, mode, setView, setMode, setPalette, toggleSidebar } = useUI();
  const { list, activeId, select } = useWorkspaces();
  const agentCounts = useAgentCounts();
  const item = (n: NavItem) => (
    <button key={n.id} type="button" aria-current={view === n.id ? "page" : undefined} onClick={() => setView(n.id)}
      className={cn(row, view === n.id && "bg-sidebar-accent text-sidebar-accent-foreground")}>
      <n.icon className="size-3.5 shrink-0" /><span className="truncate">{n.label}</span>
      {n.id === "agents" && agentCounts.needsYou > 0 && <Badge size="sm" variant="warning" className="ml-auto">{agentCounts.needsYou}</Badge>}
      {n.id === "review" && agentCounts.reviewable > 0 && <Badge size="sm" variant="secondary" className="ml-auto">{agentCounts.reviewable}</Badge>}
    </button>
  );
  return (
    <nav aria-label="Applications" className="titlebar-drag flex w-64 max-w-[45vw] shrink-0 flex-col border-r bg-sidebar">
      <div className="titlebar-nodrag flex h-8 shrink-0 items-center gap-1 px-2">
        <Logo className="size-4 text-foreground" />
        <Menu>
          <MenuTrigger render={<button type="button" aria-label="Switch workspace" className="flex min-w-0 items-center gap-1 rounded-md px-1 text-sm outline-none hover:bg-sidebar-accent focus-visible:ring-2 focus-visible:ring-sidebar-ring" />}>
            <span className="truncate font-medium">{list.find((w) => w.id === activeId)?.name ?? "Orole OS"}</span><ChevronDown className="size-3 opacity-60" />
          </MenuTrigger>
          <MenuPopup align="start" className="min-w-44">
            {list.map((w) => <MenuItem key={w.id} onClick={() => select(w.id)}>{w.name}</MenuItem>)}
            <MenuSeparator /><MenuItem onClick={() => useUI.getState().openSettings("workspaces")}>Manage workspaces…</MenuItem>
          </MenuPopup>
        </Menu>
        <span className="flex-1" />
        <Tooltip><TooltipTrigger render={<button type="button" aria-label="Hide sidebar" onClick={toggleSidebar} className="inline-flex size-6 items-center justify-center rounded-md text-muted-foreground hover:bg-sidebar-accent hover:text-foreground" />}><PanelLeftClose className="size-3.5" /></TooltipTrigger><TooltipPopup><span className="flex items-center gap-2">Hide sidebar <KbdGroup aria-label="Ctrl+B"><Kbd>Ctrl</Kbd><Kbd>B</Kbd></KbdGroup></span></TooltipPopup></Tooltip>
      </div>
      <div className="titlebar-nodrag flex min-h-0 flex-1 flex-col gap-2 px-2 pb-1">
        <Tabs value={mode} onValueChange={(next) => setMode(next as Mode)} className="gap-0">
          <TabsList size="sm" aria-label="Mode" className="w-full">
            {MODES.map((m) => (
              <TabsTab key={m.id} value={m.id} onClick={() => { if (view === "settings") setMode(m.id); }}>
                <m.icon />{m.label}
              </TabsTab>
            ))}
          </TabsList>
        </Tabs>
        <button type="button" onClick={() => setPalette(true)} aria-label="Search (Ctrl+K)" className="flex h-7 items-center gap-2 rounded-md border bg-background/40 px-2 text-muted-foreground text-sm outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring">
          <Search className="size-3.5" /><span className="flex-1 text-left">Search</span><KbdGroup aria-label="Ctrl+K"><Kbd>Ctrl</Kbd><Kbd>K</Kbd></KbdGroup>
        </button>
        {mode === "chat" ? <BotRail /> : <>
          <div className="flex flex-col gap-px">
            {PRIMARY.map(item)}
          </div>
          <SidebarProjects />
        </>}
      </div>
      <div className="titlebar-nodrag flex h-9 shrink-0 items-center gap-1 border-t px-2">
        <button type="button" onClick={() => setView("settings")} aria-current={view === "settings" ? "page" : undefined} className={cn(row, "flex-1", view === "settings" && "bg-sidebar-accent text-sidebar-accent-foreground")}>
          <SETTINGS.icon className="size-3.5" />Settings
        </button>
      </div>
    </nav>
  );
}
