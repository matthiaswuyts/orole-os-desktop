import { useRef, useState } from "react";
import { ChevronDown, ChevronRight, Zap } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Popover, PopoverPopup, PopoverTitle, PopoverTrigger } from "@/components/ui/popover";
import { RangeSlider } from "@/components/ui/range-slider";
import { Separator } from "@/components/ui/separator";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { ProviderLogo } from "@/features/agents/ProviderLogo";
import { ModelSelectorSearch } from "./ModelSelectorSearch";
import { effortChoices, effortLabel, modelLabel, providerLabel, supportedEffort } from "./model-options";
import type { HomeTaskLaunch } from "./useHomeTaskLaunch";

export function AgentModelPicker({ task }: { task: HomeTaskLaunch }) {
  const [open, setOpen] = useState(false);
  const [page, setPage] = useState<"settings" | "providers" | "models">("settings");
  const providerButton = useRef<HTMLButtonElement>(null);
  const modelButton = useRef<HTMLButtonElement>(null);
  const harness = task.harness;
  const models = harness?.models ?? [];
  const model = models.find((option) => option.id === task.model);
  const levels = model ? model.reasoning : harness?.reasoning ?? [];
  const choices = effortChoices(levels);
  const reasoning = supportedEffort(levels, task.reasoning);
  const label = modelLabel(model);

  function back() {
    const target = page === "models" ? modelButton : providerButton;
    setPage("settings");
    requestAnimationFrame(() => target.current?.focus());
  }

  return (
    <Popover open={open} onOpenChange={(next) => { setOpen(next); if (next) setPage(harness ? "settings" : "providers"); }}>
      <PopoverTrigger disabled={task.busy} aria-label="Agent, model and reasoning" render={<Button type="button" variant="ghost" className="h-9 min-w-0 gap-2 px-2 font-medium" />}>
        {harness && <ProviderLogo kind={harness.id} />}
        <span className="max-w-36 truncate">{harness ? label : "Select agent"}</span>
        {harness && levels.length > 0 && <span className="shrink-0 text-muted-foreground">{effortLabel(reasoning)}</span>}
        <ChevronDown aria-hidden className="shrink-0 opacity-60" />
      </PopoverTrigger>
      <PopoverPopup align="end" sideOffset={8} className="w-80 max-w-[calc(100vw-2rem)] motion-reduce:transition-none [&_[data-slot=popover-viewport]]:p-0">
        <PopoverTitle className="sr-only">Agent, model and reasoning</PopoverTitle>
        {page === "providers" ? (
          <ModelSelectorSearch
            kind="providers"
            disabled={task.busy}
            selected={harness?.id ?? ""}
            options={task.harnesses.map((provider) => ({ value: provider.id, label: providerLabel(provider), icon: <ProviderLogo kind={provider.id} />, disabled: !provider.available, description: provider.available ? undefined : provider.unavailableReason || "Not installed" }))}
            onBack={back}
            onSelect={(id) => {
              const provider = task.harnesses.find((option) => option.id === id);
              if (!provider?.available || task.busy) return;
              if (id !== harness?.id) task.chooseAgent(id);
              setPage("settings");
              requestAnimationFrame(() => providerButton.current?.focus());
            }}
          />
        ) : page === "models" ? (
          <ModelSelectorSearch
            kind="models"
            disabled={task.busy}
            selected={task.model}
            options={models.map((option) => ({ value: option.id, label: modelLabel(option), description: option.default ? "Provider default" : undefined }))}
            onBack={back}
            onSelect={(id) => {
              const next = models.find((option) => option.id === id);
              if (!harness || !next || task.busy) return;
              task.chooseAgent(harness.id, next.id);
              setPage("settings");
              requestAnimationFrame(() => modelButton.current?.focus());
            }}
          />
        ) : (
          <div className="flex flex-col">
            <div className="flex flex-col gap-3 p-3">
              <div className="flex items-center gap-2">
                <Tooltip>
                  <TooltipTrigger render={<Button ref={providerButton} type="button" variant="ghost" size="icon" disabled={task.busy} aria-label="Change provider" onClick={() => setPage("providers")}>
                    {harness ? <ProviderLogo kind={harness.id} /> : <ChevronDown aria-hidden />}
                  </Button>} />
                  <TooltipPopup>{harness ? providerLabel(harness) : "Agent"} · Change provider</TooltipPopup>
                </Tooltip>
                <div className="flex min-w-0 flex-1 flex-col">
                  <span className="font-medium">{levels.length ? effortLabel(reasoning) : harness ? providerLabel(harness) : "Select agent"}</span>
                  <span className="text-xs text-muted-foreground">{levels.length ? "Reasoning effort" : "No effort control reported"}</span>
                </div>
                {levels.length > 0 && <Zap aria-hidden className="mr-1 size-4 text-muted-foreground" />}
              </div>
              {levels.length > 0 && <RangeSlider aria-label="Reasoning effort" min={0} max={choices.length - 1} step={1} value={choices.indexOf(reasoning)} disabled={task.busy} formatValueText={(index) => effortLabel(choices[index] ?? "")} onValueChange={(index) => { if (harness && !task.busy) task.chooseAgent(harness.id, task.model, choices[index] ?? ""); }} />}
            </div>
            <Separator />
            <div className="p-1.5">
              <Button ref={modelButton} type="button" variant="ghost" className="h-10 w-full justify-between gap-3 px-2.5" aria-label="Change model" disabled={task.busy || !harness} onClick={() => setPage("models")}>
                <span className="text-muted-foreground">Model</span>
                <span className="ml-auto truncate">{label}</span>
                <ChevronRight aria-hidden className="shrink-0 text-muted-foreground" />
              </Button>
            </div>
          </div>
        )}
      </PopoverPopup>
    </Popover>
  );
}
