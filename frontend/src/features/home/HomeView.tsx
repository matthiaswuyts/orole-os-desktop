import { PlusIcon } from "lucide-react";
import { AsciiBand, BAND_HEIGHT } from "@/components/ascii/AsciiBackground";
import { Button } from "@/components/ui/button";
import { Popover, PopoverPopup, PopoverTrigger } from "@/components/ui/popover";
import { WidgetGrid } from "@/components/widgets/WidgetGrid";
import { useWidgetRegistry } from "@/components/widgets/registry";
import { useDashboard } from "@/stores/dashboard";
import { HomeAgentComposer } from "@/features/home/HomeAgentComposer";
import { Failure } from "@/features/mcp/shared";

/** The ASCII scene, task composer, and the person's own grid of widgets. */
export function HomeView() {
  const { widgets: items, addWidget, error } = useDashboard();
  const widgets = useWidgetRegistry();
  return (
    <div className="absolute inset-0 overflow-y-auto bg-background">
      <div className="relative min-h-full">
        <AsciiBand />
        <div className="relative mx-auto flex w-full max-w-[1000px] flex-col items-center px-4 pb-10 sm:px-6" style={{ paddingTop: `calc(${BAND_HEIGHT} - 84px)` }}>
          <h1 className="mb-3 text-balance text-center font-heading font-semibold text-2xl tracking-tight [text-shadow:0_0_6px_var(--background),0_0_16px_var(--background)]">What should your agents work on?</h1>
          <HomeAgentComposer />
        </div>
        <div className="relative mx-auto w-full max-w-[1180px] px-6 pb-12">
          <div className="mb-2 flex items-center justify-between">
            <h2 className="font-medium text-muted-foreground text-xs">Home</h2>
            <Popover>
              <PopoverTrigger render={<Button variant="ghost" size="xs" />}><PlusIcon />Customize</PopoverTrigger>
              <PopoverPopup align="end" className="max-h-96 w-72 overflow-y-auto">
                <div className="-m-1 flex flex-col">
                  {widgets.map((w) => (
                    <button key={w.type} type="button" onClick={() => addWidget(w.type, w.w, w.h)} className="flex flex-col rounded-md px-2 py-1.5 text-left outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring">
                      <span className="text-sm">{w.title}</span><span className="text-muted-foreground text-xs">{w.description}</span>
                    </button>
                  ))}
                </div>
              </PopoverPopup>
            </Popover>
          </div>
          <Failure error={error ? `Dashboard layout could not be loaded or saved: ${error}` : null} />
          {items.length === 0 && <p className="p-6 text-center text-muted-foreground">No widgets yet. Use “Customize”.</p>}
          <WidgetGrid />
        </div>
      </div>
    </div>
  );
}
