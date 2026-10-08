import { useId, useRef, useState } from "react";
import { Brain, ChevronsUpDown } from "lucide-react";
import type { LaunchOptions } from "@/mygo";
import { Button } from "@/components/ui/button";
import { Dialog, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Menu, MenuGroup, MenuGroupLabel, MenuItem, MenuPopup, MenuRadioGroup, MenuRadioItem, MenuSeparator, MenuSub, MenuSubPopup, MenuSubTrigger, MenuTrigger } from "@/components/ui/menu";
import { Slider } from "@/components/ui/slider";
import { ProviderLogo } from "./ProviderLogo";

type Harness = LaunchOptions["harnesses"][number];

export function LaunchModelMenu({ harnesses, harness, model, reasoning, disabled, onHarness, onModel, onReasoning }: {
  harnesses: Harness[];
  harness?: Harness;
  model: string;
  reasoning: string;
  disabled: boolean;
  onHarness: (kind: string) => void;
  onModel: (model: string) => void;
  onReasoning: (reasoning: string) => void;
}) {
  const id = useId();
  const [modelOpen, setModelOpen] = useState(false);
  const slider = useRef<HTMLDivElement>(null);
  const efforts = ["", ...(harness?.reasoning ?? [])];
  const effortIndex = Math.max(0, efforts.indexOf(reasoning));
  const effortLabel = (index: number) => efforts[index] || "Harness default";
  return <>
    <Menu>
      <MenuTrigger render={<Button type="button" variant="ghost" size="sm" disabled={disabled} className="max-w-full sm:max-w-56" />} aria-label={`Agent configuration: ${harness?.name ?? "Choose harness"}`}>
        <ProviderLogo kind={harness?.id ?? ""} /><span className="truncate">{model || harness?.name || "Choose harness"}</span><ChevronsUpDown />
      </MenuTrigger>
      <MenuPopup align="end" className="w-64">
        <MenuGroup><MenuGroupLabel>Agent configuration</MenuGroupLabel>
          <MenuSub><MenuSubTrigger><ProviderLogo kind={harness?.id ?? ""} />Harness<span className="ml-auto truncate text-xs text-muted-foreground">{harness?.name}</span></MenuSubTrigger>
            <MenuSubPopup className="w-64"><MenuRadioGroup value={harness?.id ?? ""} onValueChange={onHarness}>
              {harnesses.map((item) => <MenuRadioItem key={item.id} value={item.id} disabled={!item.available}><span className="flex items-center gap-2"><ProviderLogo kind={item.id} /><span>{item.name}{!item.available && <span className="block text-xs text-muted-foreground">{item.unavailableReason || "Not installed"}</span>}</span></span></MenuRadioItem>)}
            </MenuRadioGroup></MenuSubPopup>
          </MenuSub>
          <MenuSub><MenuSubTrigger disabled={!harness?.customModel}><ProviderLogo kind={harness?.id ?? ""} />Model<span className="ml-auto max-w-28 truncate text-xs text-muted-foreground">{model || "Default"}</span></MenuSubTrigger>
            <MenuSubPopup className="w-64"><MenuRadioGroup value={model} onValueChange={onModel}><MenuRadioItem value="">Harness default</MenuRadioItem>{model && <MenuRadioItem value={model}>{model}</MenuRadioItem>}</MenuRadioGroup><MenuSeparator /><MenuItem onClick={() => setModelOpen(true)}>Enter model ID…</MenuItem></MenuSubPopup>
          </MenuSub>
          <MenuSub><MenuSubTrigger disabled={!harness?.reasoning.length}><Brain />Reasoning<span className="ml-auto text-xs text-muted-foreground">{reasoning || "Default"}</span></MenuSubTrigger>
            <MenuSubPopup className="w-64">
              <MenuItem closeOnClick={false} onClick={() => {
                // Let the menu finish focusing its activated item before entering the range input.
                requestAnimationFrame(() => slider.current?.querySelector<HTMLInputElement>('input[type="range"]')?.focus());
              }}>Adjust effort<span className="ml-auto text-xs text-muted-foreground">{effortLabel(effortIndex)}</span></MenuItem>
              <div ref={slider} className="px-3 pb-3 pt-2" onKeyDown={(event) => { if (event.key !== "Escape") event.stopPropagation(); }}>
                <Slider aria-label="Reasoning effort" aria-valuetext={effortLabel(effortIndex)} min={0} max={efforts.length - 1} step={1} value={[effortIndex]} onValueChange={(value) => onReasoning(efforts[typeof value === "number" ? value : value[0]!] ?? "")} />
                <div className="mt-2 flex justify-between text-xs text-muted-foreground"><span>Default</span><span>{efforts.at(-1)}</span></div>
              </div>
              <MenuItem onClick={() => onReasoning("")}>Use harness default</MenuItem>
            </MenuSubPopup>
          </MenuSub>
        </MenuGroup>
      </MenuPopup>
    </Menu>
    <Dialog open={modelOpen} onOpenChange={setModelOpen}><DialogPopup className="p-5"><DialogTitle>Choose model</DialogTitle><Field className="mt-4"><FieldLabel htmlFor={`${id}-model`}>Model ID</FieldLabel><Input id={`${id}-model`} value={model} onChange={(event) => onModel(event.target.value)} autoComplete="off" placeholder="Harness default" /><FieldDescription>Enter an ID supported by this harness and your account. Leave empty for its configured default. The CLI does not advertise a model catalog.</FieldDescription></Field><Button type="button" className="mt-4 self-end" onClick={() => setModelOpen(false)}>Use model</Button></DialogPopup></Dialog>
  </>;
}
