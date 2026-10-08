import { Bell, ChevronDown, EllipsisIcon, PanelLeftClose, Search } from "lucide-react";
import { MORE, PRIMARY, SETTINGS, type NavItem } from "@/lib/nav";
import { cn } from "@/lib/utils";
import { useUI } from "@/stores/ui";
import { useDaemon } from "@/stores/daemon";
import { useWorkspaces } from "@/stores/workspaces";
import { Kbd } from "@/components/ui/kbd";
import { Menu, MenuItem, MenuPopup, MenuSeparator, MenuTrigger } from "@/components/ui/menu";
import { Tip } from "@/components/tip";
import { SidebarProjects } from "./SidebarProjects";
import { Logo } from "./Logo";

const row = "flex h-side-row w-full items-center gap-2 rounded-md px-2 text-left text-sidebar-foreground text-sm outline-none hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring";

/** Sidebar after Shipyard's app-sidebar: notifications, search, primary rows, projects, machines, settings. */
export function Sidebar() {
  const { view, setView, setPalette, toggleSidebar } = useUI();
  const link = useDaemon((s) => s.link);
  const { list, activeId, select } = useWorkspaces();
  const item = (n: NavItem) => (
    <button key={n.id} type="button" aria-current={view === n.id ? "page" : undefined} onClick={() => setView(n.id)}
      className={cn(row, view === n.id && "bg-sidebar-accent text-sidebar-accent-foreground")}>
      <n.icon className="size-3.5 shrink-0" /><span className="truncate">{n.label}</span>
    </button>
  );
  const moreActive = MORE.some((m) => m.id === view);
  return (
    <nav aria-label="Applications" className="titlebar-drag flex w-[190px] shrink-0 flex-col border-r bg-sidebar" style={{ paddingTop: "var(--mygo-titlebar-height, 0px)" }}>
      <div className="titlebar-nodrag flex h-8 shrink-0 items-center gap-1 px-2">
        <Logo className="size-4 text-foreground" />
        <Menu>
          <MenuTrigger render={<button type="button" aria-label="Switch workspace" className="flex min-w-0 items-center gap-1 rounded-md px-1 text-sm outline-none hover:bg-sidebar-accent focus-visible:ring-2 focus-visible:ring-sidebar-ring" />}>
            <span className="truncate font-medium">{list.find((w) => w.id === activeId)?.name ?? "Orole OS"}</span><ChevronDown className="size-3 opacity-60" />
          </MenuTrigger>
          <MenuPopup align="start" className="min-w-44">
            {list.map((w) => <MenuItem key={w.id} onClick={() => select(w.id)}>{w.name}</MenuItem>)}
            <MenuSeparator /><MenuItem onClick={() => setView("settings")}>Manage workspaces…</MenuItem>
          </MenuPopup>
        </Menu>
        <span className="flex-1" />
        <Tip label="Notifications" side="bottom"><button type="button" aria-label="Notifications, none unread" className="inline-flex size-6 items-center justify-center rounded-md text-muted-foreground hover:bg-sidebar-accent hover:text-foreground"><Bell className="size-3.5" /></button></Tip>
      </div>
      <div className="titlebar-nodrag flex min-h-0 flex-1 flex-col gap-2 px-2 pb-1">
        <button type="button" onClick={() => setPalette(true)} aria-label="Search (Ctrl+K)" className="flex h-7 items-center gap-2 rounded-md border bg-background/40 px-2 text-muted-foreground text-sm outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring">
          <Search className="size-3.5" /><span className="flex-1 text-left">Search</span><Kbd>⌘K</Kbd>
        </button>
        <div className="flex flex-col gap-px">
          {PRIMARY.map(item)}
          <Menu>
            <MenuTrigger render={<button type="button" className={cn(row, moreActive && "bg-sidebar-accent text-sidebar-accent-foreground")} />}>
              <EllipsisIcon className="size-3.5" />More
            </MenuTrigger>
            <MenuPopup align="start" side="right" className="min-w-44">
              {MORE.map((m) => <MenuItem key={m.id} onClick={() => setView(m.id)}><m.icon className="size-3.5" />{m.label}</MenuItem>)}
            </MenuPopup>
          </Menu>
        </div>
        <SidebarProjects />
      </div>
      <div className="titlebar-nodrag flex h-9 shrink-0 items-center gap-1 border-t px-2">
        <button type="button" onClick={() => setView("settings")} aria-current={view === "settings" ? "page" : undefined} className={cn(row, "flex-1", view === "settings" && "bg-sidebar-accent text-sidebar-accent-foreground")}>
          <SETTINGS.icon className="size-3.5" />Settings
        </button>
        <span className={cn("size-1.5 shrink-0 rounded-full", link === "connected" ? "bg-success" : link === "offline" ? "bg-destructive" : "bg-muted-foreground/60")} role="status" aria-label={`Daemon ${link}`} />
        <Tip label="Hide sidebar" side="top"><button type="button" aria-label="Hide sidebar" onClick={toggleSidebar} className="inline-flex size-6 items-center justify-center rounded-md text-muted-foreground hover:bg-sidebar-accent hover:text-foreground"><PanelLeftClose className="size-3.5" /></button></Tip>
      </div>
    </nav>
  );
}
