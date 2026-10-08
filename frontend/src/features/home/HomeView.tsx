import { PlusIcon, Search, SquareTerminal, Settings as SettingsIcon } from "lucide-react";
import { AsciiBand, BAND_HEIGHT } from "@/components/ascii/AsciiBackground";
import { Button } from "@/components/ui/button";
import { Frame, FramePanel } from "@/components/ui/frame";
import { Kbd } from "@/components/ui/kbd";
import { Popover, PopoverPopup, PopoverTrigger } from "@/components/ui/popover";
import { WidgetGrid } from "@/components/widgets/WidgetGrid";
import { widgets } from "@/components/widgets/registry";
import { useUI } from "@/stores/ui";
import { useWorkspaces } from "@/stores/workspaces";

/**
 * Home after Shipyard's views/home/home-view.tsx: the scene across the top, dissolving into the
 * page, a heading and one launcher at the seam, then the person's own grid of widgets.
 */
export function HomeView() {
  const { list, activeId, addWidget } = useWorkspaces();
  const { setPalette, setView } = useUI();
  const ws = list.find((w) => w.id === activeId);
  return (
    <div className="absolute inset-0 overflow-y-auto bg-background">
      <div className="relative min-h-full">
        <AsciiBand />
        <div className="relative mx-auto flex w-full max-w-[640px] flex-col items-center px-6 pb-10" style={{ paddingTop: `calc(${BAND_HEIGHT} - 84px)` }}>
          <h1 className="mb-4 text-balance text-center font-heading font-semibold text-2xl tracking-tight [text-shadow:0_0_6px_var(--background),0_0_16px_var(--background)]">Your personal operating environment</h1>
          <Frame className="w-full shadow-lg/5">
            <FramePanel className="p-0">
              <button type="button" onClick={() => setPalette(true)} className="flex h-12 w-full items-center gap-2 rounded-[inherit] px-3.5 text-left text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring">
                <Search className="size-4" /><span className="flex-1">Search, open an app or run a command…</span><Kbd>⌘K</Kbd>
              </button>
              <div className="flex items-center gap-1 border-t px-2 py-1.5">
                <Button variant="ghost" size="xs" onClick={() => setView("terminals")}><SquareTerminal />New terminal</Button>
                <Button variant="ghost" size="xs" onClick={() => setView("settings")}><SettingsIcon />Settings</Button>
              </div>
            </FramePanel>
          </Frame>
        </div>
        <div className="relative mx-auto w-full max-w-[1180px] px-6 pb-12">
          <div className="mb-2 flex items-center justify-between">
            <h2 className="font-medium text-muted-foreground text-xs">{ws?.name ?? "Home"}</h2>
            <Popover>
              <PopoverTrigger render={<Button variant="ghost" size="xs" />}><PlusIcon />Customize</PopoverTrigger>
              <PopoverPopup align="end" className="w-72">
                <div className="-m-1 flex flex-col">
                  {widgets.map((w) => (
                    <button key={w.type} type="button" onClick={() => addWidget(w.type, w.w, w.h)} className="flex flex-col rounded-md px-2 py-1.5 text-left hover:bg-accent">
                      <span className="text-sm">{w.title}</span><span className="text-muted-foreground text-xs">{w.description}</span>
                    </button>
                  ))}
                </div>
              </PopoverPopup>
            </Popover>
          </div>
          {ws && ws.layout.widgets.length === 0 && <p className="p-6 text-center text-muted-foreground">This workspace has no widgets. Use “Customize”.</p>}
          <WidgetGrid />
        </div>
      </div>
    </div>
  );
}
