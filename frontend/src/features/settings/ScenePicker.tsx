import { Check } from "lucide-react";
import { ScenePreview } from "@/components/ascii/ScenePreview";
import { SCENES } from "@/components/ascii/scenes";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { useSettings } from "@/stores/settings";

export function ScenePicker() {
  const scene = useSettings((state) => state.activeAsciiScene);
  const loaded = useSettings((state) => state.loaded);
  const set = useSettings((state) => state.set);
  return (
    <div role="group" aria-label="Background scene" className="grid grid-cols-[repeat(auto-fit,minmax(140px,1fr))] gap-2 p-3">
      {SCENES.map((entry) => (
        <Button key={entry.id} variant="outline" disabled={!loaded} aria-pressed={scene === entry.id}
          onClick={() => set("ascii", { scene: entry.id })}
          className={cn("h-auto min-w-0 flex-col gap-0 overflow-hidden p-0 text-left sm:h-auto", scene === entry.id && "border-primary")}>
          <ScenePreview scene={entry.id} />
          <span className="flex w-full items-center justify-between gap-2 px-2 py-1.5 text-xs">
            {entry.label}
            {scene === entry.id && <Check aria-hidden="true" className="size-3.5 shrink-0 text-primary" />}
          </span>
        </Button>
      ))}
    </div>
  );
}
