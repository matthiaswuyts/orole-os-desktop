import { PanelLeftClose } from "lucide-react";
import { AUTOMATIONS, PRIMARY, SETTINGS, type NavItem } from "@/lib/nav";
import { cn } from "@/lib/utils";
import { useUI } from "@/stores/ui";
import { Kbd, KbdGroup } from "@/components/ui/kbd";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { SidebarProjects } from "./SidebarProjects";
import { Logo } from "./Logo";
import { useAgentCounts, useAgents } from "@/stores/agents";
import { Badge } from "@/components/ui/badge";
import { BotRail } from "./BotRail";
import { SidebarSearch } from "./SidebarSearch";
import { Desktop } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { useAutomationApprovals } from "@/features/automations/useApprovals";

const row = "flex h-side-row w-full items-center gap-2 rounded-md px-2 text-left text-sidebar-foreground text-sm outline-none hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring";

/** One sidebar: search, navigation (Code views, then Automations), the Agents (bots), the Code projects, and settings. */
export function Sidebar() {
  const view = useUI((s) => s.view);
  const setView = useUI((s) => s.setView);
  const approvals = useAutomationApprovals().length;
  const toggleSidebar = useUI((s) => s.toggleSidebar);
  const agentCounts = useAgentCounts();
  // An open task has its own highlighted row; the Tasks board is only "current" when no task is open.
  const taskOpen = useAgents((s) => s.openId !== null && (!!s.sessions[s.openId] || !!s.launches[s.openId]));
  const item = (n: NavItem) => {
    const current = view === n.id && !(n.id === "agents" && taskOpen);
    return (
      <button key={n.id} type="button" aria-current={current ? "page" : undefined} onClick={() => { if (n.id === "agents") useAgents.getState().setOpenId(null); setView(n.id); }}
        className={cn(row, current && "bg-sidebar-accent text-sidebar-accent-foreground")}>
        <n.icon className="size-3.5 shrink-0" /><span className="truncate">{n.label}</span>
        {n.id === "agents" && agentCounts.needsYou > 0 && <Badge size="sm" variant="warning" className="ml-auto">{agentCounts.needsYou}</Badge>}
        {n.id === "review" && agentCounts.reviewable > 0 && <Badge size="sm" variant="secondary" className="ml-auto">{agentCounts.reviewable}</Badge>}
        {n.id === "automations" && approvals > 0 && <Badge size="sm" variant="warning" className="ml-auto" aria-label={`${approvals} awaiting approval`}>{approvals}</Badge>}
      </button>
    );
  };
  // Desktop: isolated settings window. Browser preview has no native windows, so it keeps the in-app view.
  const openSettingsWindow = () => {
    if (!inDesktop) return setView("settings");
    Desktop.openSettings(useUI.getState().settingsSection).catch((error: unknown) => console.warn("Could not open the settings window", error));
  };
  return (
    <nav aria-label="Applications" className="titlebar-drag flex w-64 max-w-[45vw] shrink-0 flex-col border-r bg-sidebar">
      <div className="titlebar-nodrag flex h-8 shrink-0 items-center gap-1 px-2">
        <Logo className="size-4 text-foreground" />
        <span className="truncate font-medium text-sm">Orole OS</span>
        <span className="flex-1" />
        <Tooltip><TooltipTrigger render={<button type="button" aria-label="Hide sidebar" onClick={toggleSidebar} className="inline-flex size-6 items-center justify-center rounded-md text-muted-foreground hover:bg-sidebar-accent hover:text-foreground" />}><PanelLeftClose className="size-3.5" /></TooltipTrigger><TooltipPopup><span className="flex items-center gap-2">Hide sidebar <KbdGroup aria-label="Ctrl+B"><Kbd>Ctrl</Kbd><Kbd>B</Kbd></KbdGroup></span></TooltipPopup></Tooltip>
      </div>
      <div className="titlebar-nodrag flex min-h-0 flex-1 flex-col gap-2 px-2 pb-1">
        <SidebarSearch />
        <div className="flex flex-col gap-px">
          {PRIMARY.map(item)}
          {item(AUTOMATIONS)}
        </div>
        {/* Agents and Code scroll together, so a long bot list never squeezes the projects out of view. */}
        <div className="-mx-2 flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto px-2">
          <BotRail />
          <SidebarProjects />
        </div>
      </div>
      <div className="titlebar-nodrag flex h-9 shrink-0 items-center gap-1 border-t px-2">
        <button type="button" onClick={openSettingsWindow} className={cn(row, "flex-1")}>
          <SETTINGS.icon className="size-3.5" />Settings
        </button>
      </div>
    </nav>
  );
}
