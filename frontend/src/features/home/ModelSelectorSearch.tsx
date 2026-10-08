import type { ReactNode } from "react";
import { ArrowLeft, Check } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Command, CommandCollection, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command";
import { Separator } from "@/components/ui/separator";

export interface SelectorOption {
  value: string;
  label: string;
  icon?: ReactNode;
  disabled?: boolean;
  description?: string;
}

export function ModelSelectorSearch({ options, selected, kind, disabled, onBack, onSelect }: {
  options: SelectorOption[];
  selected: string;
  kind: "providers" | "models";
  disabled: boolean;
  onBack: () => void;
  onSelect: (value: string) => void;
}) {
  return (
    <Command items={options}>
      <div className="flex items-center gap-1 px-2">
        <Button type="button" variant="ghost" size="icon-sm" aria-label="Back to agent settings" onClick={onBack}><ArrowLeft aria-hidden /></Button>
        <div className="min-w-0 flex-1"><CommandInput aria-label={`Search ${kind}`} placeholder={`Search ${kind}…`} disabled={disabled} /></div>
      </div>
      <Separator />
      <CommandEmpty>{options.length ? `No matching ${kind}.` : kind === "providers" ? "No agents discovered." : "Model catalog unavailable."}</CommandEmpty>
      <CommandList className="max-h-64 overflow-y-auto" aria-label={kind === "providers" ? "Providers" : "Models"}>
        <CommandGroup>
          <CommandCollection>
            {(option: SelectorOption) => (
              <CommandItem key={option.value} value={option} disabled={disabled || option.disabled} onClick={() => onSelect(option.value)} className="min-h-10 gap-2.5 rounded-md" aria-label={option.label}>
                {option.icon}
                <span className="flex min-w-0 flex-1 flex-col">
                  <span className="truncate">{option.label}</span>
                  {option.description && <span className="text-xs text-muted-foreground">{option.description}</span>}
                </span>
                {option.value === selected && <Check aria-hidden className="size-4 shrink-0" />}
              </CommandItem>
            )}
          </CommandCollection>
        </CommandGroup>
      </CommandList>
    </Command>
  );
}
