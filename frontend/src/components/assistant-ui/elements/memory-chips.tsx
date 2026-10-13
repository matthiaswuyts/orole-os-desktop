import { BrainIcon, XIcon } from "lucide-react";
import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";
import { surfaces } from "./surfaces";

export interface MemoryChip { id: string; text: string; change: "added" | "updated" | "existing" }

/**
 * assistant-ui's memory chips (https://www.assistant-ui.com/elements/memory-chips): one pill per remembered fact, freshly
 * written ones tinted. A forget button appears on each pill only when `onForget` is supplied.
 */
export function MemoryChips({ chips, onForget, className, ...props }: Omit<ComponentProps<"div">, "children"> & {
  chips: readonly MemoryChip[];
  onForget?: (id: string) => void;
}) {
  const fresh = chips.filter((chip) => chip.change !== "existing").length;
  return (
    <div data-slot="memory-chips" className={cn("flex min-w-0 flex-col gap-1.5", className)} {...props}>
      <div className={cn(surfaces.mono, "flex items-center gap-1.5")}>
        <BrainIcon aria-hidden className="size-3.5" />
        <span>{fresh > 0 ? `remembered ${fresh}` : "memory"}</span>
      </div>
      {chips.length > 0 && <div className="flex flex-wrap gap-1.5">
        {chips.map((chip) => (
          <span key={chip.id} className={cn(
            surfaces.field, "inline-flex max-w-full items-center gap-1 px-2.5 py-0.5 text-xs animate-in fade-in-0 zoom-in-95 duration-300 motion-reduce:animate-none",
            chip.change !== "existing" && "border-info/40 bg-info/10 text-info-foreground",
          )}>
            <span className="min-w-0 truncate">{chip.text}</span>
            {onForget && <button type="button" aria-label={`Forget "${chip.text}"`} onClick={() => onForget(chip.id)} className={cn(surfaces.ghostButton, "size-4")}><XIcon aria-hidden className="size-3" /></button>}
          </span>
        ))}
      </div>}
    </div>
  );
}
