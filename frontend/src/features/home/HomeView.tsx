import { useState } from "react";
import { LayoutGrid, PlusIcon, RotateCcw } from "lucide-react";
import { AsciiBand, BAND_HEIGHT } from "@/components/ascii/AsciiBackground";
import { Button } from "@/components/ui/button";
import { Popover, PopoverPopup, PopoverTrigger } from "@/components/ui/popover";
import { WidgetGrid } from "@/components/widgets/WidgetGrid";
import { useWidgetRegistry } from "@/components/widgets/registry";
import { useDashboard } from "@/stores/dashboard";
import { HomeAgentComposer } from "@/features/home/HomeAgentComposer";
import { Failure } from "@/features/mcp/shared";

/** Widget gallery plus "Reset layout", which asks before discarding the current arrangement. */
function Customize() {
  const items = useDashboard((s) => s.widgets);
  const addWidget = useDashboard((s) => s.addWidget);
  const resetLayout = useDashboard((s) => s.resetLayout);
  const widgets = useWidgetRegistry();
  const [confirming, setConfirming] = useState(false);
  return (
    <Popover onOpenChange={(open) => { if (!open) setConfirming(false); }}>
      <PopoverTrigger render={<Button variant="ghost" size="xs" className="text-muted-foreground" />}><PlusIcon />Customize</PopoverTrigger>
      <PopoverPopup side="top" align="center" className="w-[34rem] max-w-[calc(100vw-2rem)]">
        <div className="-m-1 flex max-h-[min(32rem,70vh)] flex-col">
          <p className="px-2 pt-1 pb-2 font-medium text-sm">Add a widget</p>
          <div className="grid min-h-0 grid-cols-1 gap-1 overflow-y-auto sm:grid-cols-2">
            {widgets.map((w) => {
              const added = items.filter((item) => item.type === w.type).length;
              const Icon = w.icon ?? LayoutGrid;
              return (
                <button key={w.type} type="button" onClick={() => addWidget(w.type, w.size, w.tall)}
                  className="flex items-start gap-3 rounded-lg border border-transparent p-2.5 text-left outline-none hover:border-border hover:bg-accent/60 focus-visible:ring-2 focus-visible:ring-ring">
                  <span className="mt-0.5 inline-flex size-8 shrink-0 items-center justify-center rounded-md border bg-card text-muted-foreground" aria-hidden><Icon className="size-4" /></span>
                  <span className="flex min-w-0 flex-col gap-0.5">
                    <span className="text-sm">{w.title}</span>
                    <span className="line-clamp-2 text-muted-foreground text-xs">{w.description}</span>
                    {added > 0 && <span className="text-[11px] text-success-foreground">{added === 1 ? "On Home" : `${added} on Home`}</span>}
                  </span>
                </button>
              );
            })}
          </div>
          <div className="mt-2 flex min-h-9 items-center gap-2 border-t px-2 pt-2">
            {confirming ? <>
              <p className="flex-1 text-muted-foreground text-xs">Restore the default widgets? Notes and widget settings are cleared.</p>
              <Button size="xs" variant="ghost" onClick={() => setConfirming(false)}>Cancel</Button>
              <Button size="xs" variant="destructive" onClick={() => { resetLayout(); setConfirming(false); }}>Reset</Button>
            </> : <>
              <p className="flex-1 text-muted-foreground text-xs">Drag a widget by its heading to move it; its menu sets the size.</p>
              <Button size="xs" variant="ghost" onClick={() => setConfirming(true)}><RotateCcw />Reset layout</Button>
            </>}
          </div>
        </div>
      </PopoverPopup>
    </Popover>
  );
}

/** The ASCII scene, task composer, and the person's own grid of widgets in one centered column. */
export function HomeView() {
  const count = useDashboard((s) => s.widgets.length);
  const error = useDashboard((s) => s.error);
  return (
    <div className="absolute inset-0 overflow-y-auto bg-background">
      <div className="relative min-h-full">
        <AsciiBand />
        <div className="relative mx-auto flex w-full max-w-[1000px] flex-col items-center px-4 pb-10 sm:px-6" style={{ paddingTop: `calc(${BAND_HEIGHT} - 84px)` }}>
          <h1 className="mb-3 text-balance text-center font-heading font-semibold text-2xl tracking-tight [text-shadow:0_0_6px_var(--background),0_0_16px_var(--background)]">What should your agents work on?</h1>
          <HomeAgentComposer />
        </div>
        <div className="relative mx-auto w-full max-w-[1040px] px-4 pt-2 pb-16 sm:px-6">
          <Failure error={error ? `Dashboard layout could not be loaded or saved: ${error}` : null} />
          {count === 0 && <p className="p-6 text-center text-muted-foreground">No widgets yet. Use “Customize”.</p>}
          <WidgetGrid />
          <div className="mt-6 flex justify-center">
            <Customize />
          </div>
        </div>
      </div>
    </div>
  );
}
