import { useId, useRef, useState, type KeyboardEvent } from "react";
import { CheckIcon, ChevronDownIcon, SearchIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Kbd } from "@/components/ui/kbd";
import { Popover, PopoverPopup, PopoverTitle, PopoverTrigger } from "@/components/ui/popover";
import { usePromptInput } from "@/components/blocks/prompt-input/context";
import { useEffortLevel } from "@/components/blocks/prompt-input/effort";
import { EffortWidth, FlowText, PromptInputEffortPanel } from "@/components/blocks/prompt-input/prompt-controls";
import { ProviderLogo } from "@/features/agents/ProviderLogo";
import { cn } from "@/lib/utils";
import { effortChoices, effortLabel, modelContext, modelLabel, modelTitle, providerLabel } from "./model-options";
import type { HomeTaskLaunch } from "./useHomeTaskLaunch";

/** Above this many models the list gets a search field. */
const SEARCH_AT = 7;
const row = "flex min-h-9 w-full items-center gap-2.5 rounded-md px-2 text-start text-sm outline-none select-none hover:bg-accent hover:text-accent-foreground focus-visible:bg-accent focus-visible:text-accent-foreground disabled:pointer-events-none disabled:opacity-64 aria-checked:bg-accent/60 pointer-coarse:min-h-11";

/** "2.1.250 (Claude Code)" → "v2.1.250"; non-numeric versions ("omp/18.4.10") are shown as reported. */
function versionLabel(version: string): string {
  const clean = version.replace(/\s*\(.*\)\s*$/, "").trim();
  return /^\d/.test(clean) ? `v${clean}` : clean || "Installed";
}

/** Arrow keys move the choice through a radio list and keep focus on the chosen row. */
function moveChoice(event: KeyboardEvent<HTMLElement>, values: string[], current: string, choose: (value: string) => void) {
  const step = event.key === "ArrowDown" ? 1 : event.key === "ArrowUp" ? -1 : 0;
  if (!step || !values.length) return;
  event.preventDefault();
  const at = values.indexOf(current);
  const next = values[(at + step + values.length) % values.length]!;
  choose(next);
  const list = event.currentTarget;
  requestAnimationFrame(() => list.querySelector<HTMLElement>("[aria-checked=true]")?.focus());
}

/** Agent (harness), model and reasoning effort in one menu: agents on the left, their models and effort on the right. */
export function AgentModelPicker({ task }: { task: HomeTaskLaunch }) {
  const composer = usePromptInput("AgentModelPicker");
  const id = useId();
  const [query, setQuery] = useState("");
  const selectedModel = useRef<HTMLButtonElement>(null);
  const harness = task.harness;
  const models = harness?.models ?? [];
  const model = models.find((option) => option.id === task.model);
  const levels = effortChoices(model ? model.reasoning : harness?.reasoning ?? []).map((value) => ({ value, label: effortLabel(value) }));
  const hasEffort = levels.length > 1;
  const { current: level, direction } = useEffortLevel(levels, task.reasoning);
  const needle = query.trim().toLowerCase();
  const shown = needle ? models.filter((option) => `${modelTitle(option)} ${option.id}`.toLowerCase().includes(needle)) : models;
  const available = task.harnesses.filter((option) => option.available).map((option) => option.id);

  const chooseHarness = (id: string) => {
    if (task.busy || id === harness?.id || !available.includes(id)) return;
    setQuery("");
    task.chooseAgent(id);
  };
  const chooseModel = (id: string) => { if (harness && !task.busy) task.chooseAgent(harness.id, id); };

  return <Popover onOpenChange={(open) => { if (!open) setQuery(""); }}>
    <PopoverTrigger disabled={task.busy} aria-label="Agent, model and reasoning"
      render={<Button type="button" variant="ghost" size="sm" className="min-w-0 rounded-full font-medium data-popup-open:bg-accent" />}>
      {harness && <ProviderLogo kind={harness.id} />}
      <span className="max-w-52 truncate">{harness ? modelLabel(model) : "Select agent"}</span>
      {harness && modelContext(model) && <span className="shrink-0 font-normal text-muted-foreground">{modelContext(model)}</span>}
      {harness && hasEffort && <span className="grid justify-items-start font-normal text-muted-foreground *:col-start-1 *:row-start-1">
        <EffortWidth levels={levels} />
        <FlowText value={level?.label ?? ""} direction={direction} />
      </span>}
      <ChevronDownIcon aria-hidden className="shrink-0 opacity-60" />
    </PopoverTrigger>
    <PopoverPopup side="top" align="end" sideOffset={8} initialFocus={selectedModel} finalFocus={composer.textareaRef}
      className="w-[38rem] max-w-[calc(100vw-2rem)] motion-reduce:transition-none [&_[data-slot=popover-viewport]]:p-0"
      onKeyDown={(event) => {
        // 1–9 pick a visible model, unless the search field is taking the keystroke.
        const position = Number(event.key);
        if (event.target instanceof HTMLInputElement || !Number.isInteger(position) || position < 1 || position > Math.min(9, shown.length) || event.metaKey || event.ctrlKey || event.altKey) return;
        event.preventDefault();
        chooseModel(shown[position - 1]!.id);
      }}>
      <PopoverTitle className="sr-only">Agent, model and reasoning</PopoverTitle>
      <div className="flex min-h-0 max-sm:flex-col">
        <section aria-labelledby={`${id}-agents`} className="flex shrink-0 flex-col gap-0.5 border-border p-1.5 max-sm:border-b sm:w-48 sm:border-e">
          <h3 id={`${id}-agents`} className="px-2 py-1.5 text-xs font-medium text-muted-foreground">Agent</h3>
          {task.harnesses.length === 0 ? <p className="px-2 py-1.5 text-sm text-muted-foreground">No agents discovered.</p>
            : <div role="radiogroup" aria-label="Agent" className="flex max-h-72 flex-col gap-0.5 overflow-y-auto" onKeyDown={(event) => moveChoice(event, available, harness?.id ?? "", chooseHarness)}>
              {task.harnesses.map((option) => {
                const selected = option.id === harness?.id;
                return <button key={option.id} type="button" role="radio" aria-checked={selected} tabIndex={selected || (!harness && option.available) ? 0 : -1}
                  disabled={!option.available || task.busy} onClick={() => chooseHarness(option.id)} className={cn(row, "py-1.5")}>
                  <span className="flex size-5 shrink-0 items-center justify-center"><ProviderLogo kind={option.id} /></span>
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span className="truncate font-medium">{providerLabel(option)}</span>
                    <span className="truncate text-xs text-muted-foreground">{option.available ? versionLabel(option.version) : option.unavailableReason || "Not installed"}</span>
                  </span>
                  {selected && <CheckIcon aria-hidden className="size-4 shrink-0" />}
                </button>;
              })}
            </div>}
        </section>
        <div className="flex min-w-0 flex-1 flex-col">
          {models.length >= SEARCH_AT && <label className="flex items-center gap-2 border-b px-3">
            <SearchIcon aria-hidden className="size-4 shrink-0 text-muted-foreground" />
            <input type="search" value={query} onChange={(event) => setQuery(event.target.value)} aria-label="Search models"
              placeholder={`Search ${harness ? providerLabel(harness) : ""} models…`}
              className="h-10 min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground" />
          </label>}
          <section aria-labelledby={`${id}-models`} className="flex min-h-0 flex-col p-1.5">
            <h3 id={`${id}-models`} className="px-2 py-1.5 text-xs font-medium text-muted-foreground">Model</h3>
            {!harness ? <p className="px-2 py-1.5 text-sm text-muted-foreground">Pick an installed agent first.</p>
              : !models.length ? <p className="px-2 py-1.5 text-sm text-muted-foreground">{providerLabel(harness)} reports no model catalog; it uses its own default.</p>
              : !shown.length ? <p className="px-2 py-1.5 text-sm text-muted-foreground">No matching models.</p>
              : <div role="radiogroup" aria-label="Model" className="flex max-h-56 flex-col gap-0.5 overflow-y-auto" onKeyDown={(event) => moveChoice(event, shown.map((option) => option.id), task.model, chooseModel)}>
                {shown.map((option, index) => {
                  const selected = option.id === task.model;
                  return <button key={option.id} ref={selected ? selectedModel : undefined} type="button" role="radio" aria-checked={selected}
                    tabIndex={selected || (!shown.some((item) => item.id === task.model) && index === 0) ? 0 : -1} disabled={task.busy}
                    onClick={() => chooseModel(option.id)} className={row}>
                    <span className="min-w-0 flex-1 truncate">{modelLabel(option)}</span>
                    {modelContext(option) && <span className="shrink-0 text-xs text-muted-foreground">{modelContext(option)} context</span>}
                    {option.default && <span className="shrink-0 text-xs text-muted-foreground">Default</span>}
                    {selected ? <CheckIcon aria-hidden className="size-4 shrink-0" /> : index < 9 && <Kbd aria-hidden>{String(index + 1)}</Kbd>}
                  </button>;
                })}
              </div>}
          </section>
          <div className="mt-auto border-t p-3">
            {hasEffort
              ? <PromptInputEffortPanel levels={levels} value={task.reasoning} disabled={task.busy || !harness} label="Reasoning effort"
                description="How long the agent thinks before it acts. Auto leaves it to the agent; higher is smarter but slower."
                onValueChange={(value) => { if (harness && !task.busy) task.chooseAgent(harness.id, task.model, value); }} />
              : <p className="text-sm text-muted-foreground">{harness ? "This model reports no effort control." : "Effort depends on the agent and model."}</p>}
          </div>
        </div>
      </div>
    </PopoverPopup>
  </Popover>;
}
