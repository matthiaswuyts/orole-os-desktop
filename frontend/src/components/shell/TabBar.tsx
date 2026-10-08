import { Bell, Check, ChevronsUpDown, HouseIcon, Plus, Search } from "lucide-react";
import { useState } from "react";
import { useWorkspaces } from "@/stores/workspaces";
import { useUI } from "@/stores/ui";
import { useDaemon } from "@/stores/daemon";
import { Menu, MenuItem, MenuPopup, MenuSeparator, MenuTrigger } from "@/components/ui/menu";
import { Button } from "@/components/ui/button";
import { Dialog, DialogFooter, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Kbd } from "@/components/ui/kbd";
import { Tip } from "@/components/tip";
import { cn } from "@/lib/utils";
import { Logo } from "./Logo";

/**
 * The strip over the window (Shipyard's tab bar: h-10, bg-sidebar, hairline below). MyGo's hidden
 * title bar keeps the native window controls at the right edge; the CSS inset variables keep clear.
 */
export function TabBar() {
  const { list, activeId, select, create } = useWorkspaces();
  const { view, setView, setPalette } = useUI();
  const link = useDaemon((s) => s.link);
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const active = list.find((w) => w.id === activeId);
  return (
    <div className="titlebar-drag flex shrink-0 items-stretch border-b bg-sidebar" style={{ height: "var(--titlebar-h)", paddingLeft: "var(--mygo-titlebar-inset-left, 0px)", paddingRight: "var(--mygo-titlebar-inset-right, 0px)" }}>
      <div className="flex items-center gap-2 pr-3 pl-3 text-foreground"><Logo className="size-4" /></div>
      <div className="titlebar-nodrag flex items-stretch">
        <Tip label="Home" side="bottom">
          <button type="button" aria-label="Home" aria-selected={view === "home"} onClick={() => setView("home")}
            className={cn("relative flex w-10 items-center justify-center border-x outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset", view === "home" ? "bg-background text-foreground" : "text-muted-foreground hover:bg-background/40 hover:text-foreground")}>
            {view === "home" && <span className="absolute inset-x-0 top-0 h-px bg-foreground/50" />}
            <HouseIcon className="size-3.5" />
          </button>
        </Tip>
        <Menu>
          <MenuTrigger render={<button type="button" aria-label="Switch workspace" className="flex items-center gap-1.5 px-3 text-sm text-muted-foreground outline-none hover:bg-background/40 hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset" />}>
            {active?.name ?? "Workspace"}<ChevronsUpDown className="size-3 opacity-60" />
          </MenuTrigger>
          <MenuPopup align="start" className="min-w-48">
            {list.map((w) => (
              <MenuItem key={w.id} onClick={() => select(w.id)}>
                <Check className={cn("size-3.5", w.id === activeId ? "opacity-100" : "opacity-0")} />{w.name}
              </MenuItem>
            ))}
            <MenuSeparator />
            <MenuItem onClick={() => setCreating(true)}><Plus className="size-3.5" />New workspace…</MenuItem>
          </MenuPopup>
        </Menu>
      </div>
      <div className="flex-1" />
      <div className="titlebar-nodrag flex items-center gap-1 pr-2">
        <button type="button" onClick={() => setPalette(true)} aria-label="Open command palette"
          className="flex h-7 items-center gap-2 rounded-md bg-accent px-2.5 text-muted-foreground text-xs outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring">
          <Search className="size-3.5" />Search<Kbd>⌘K</Kbd>
        </button>
        <Tip label="Notifications" side="bottom"><Button variant="ghost" size="icon-sm" aria-label="Notifications, none unread"><Bell /></Button></Tip>
        <span role="status" aria-label={`Daemon ${link}`} className="flex items-center gap-1.5 px-1.5 text-muted-foreground text-xs">
          <span className={cn("size-1.5 rounded-full", link === "connected" ? "bg-success" : link === "offline" ? "bg-destructive" : "bg-muted-foreground/60")} />
          {link === "connected" ? "Daemon" : link === "offline" ? "Daemon offline" : link === "preview" ? "Preview" : "Connecting"}
        </span>
      </div>
      <Dialog open={creating} onOpenChange={setCreating}>
        <DialogPopup>
          <DialogHeader><DialogTitle>New workspace</DialogTitle></DialogHeader>
          <DialogPanel>
            <form id="ws-form" onSubmit={(e) => { e.preventDefault(); if (name.trim()) { void create(name.trim()); setName(""); setCreating(false); } }}>
              <Input autoFocus aria-label="Workspace name" placeholder="e.g. Personal, Infrastructure" value={name} onChange={(e) => setName(e.target.value)} maxLength={80} />
            </form>
          </DialogPanel>
          <DialogFooter><Button type="submit" form="ws-form" disabled={!name.trim()}>Create</Button></DialogFooter>
        </DialogPopup>
      </Dialog>
    </div>
  );
}
