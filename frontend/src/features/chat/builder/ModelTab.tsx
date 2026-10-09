import { useEffect, useMemo, type Dispatch } from "react";
import type { Caps, ProviderStatus } from "@/mygo";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Combobox, ComboboxEmpty, ComboboxInput, ComboboxItem, ComboboxList, ComboboxPopup } from "@/components/ui/combobox";
import { Select, SelectGroup, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { CapBadges } from "@/features/settings/ModelDefaults";
import { useUI } from "@/stores/ui";
import { useLLM, type ModelCatalog } from "@/stores/llm";
import type { Action, FormState } from "./botForm";
import { Block } from "./controls";

/** One selectable model across connected providers. The empty value is "use the defaults". */
export interface Option {
  value: string;
  label: string;
  provider: string;
  caps: Caps | null;
  reasoning: string[];
  kind: "default" | "model" | "missing";
}

const matches = (option: Option, query: string) => {
  const haystack = `${option.label} ${option.value} ${option.provider}`.toLowerCase();
  return query.toLowerCase().split(/\s+/).every((token) => haystack.includes(token));
};

/** Provider-reported reasoning levels of the model a bot would use, or [] when unknown. */
export function useModelOptions(selected: string): ModelOptions {
  const providers = useLLM((s) => s.providers);
  const defaults = useLLM((s) => s.defaults);
  const catalogs = useLLM((s) => s.models);
  const connected = useMemo(() => providers.filter((p) => p.connected), [providers]);
  const connectedKey = connected.map((p) => p.id).join("\n");
  useEffect(() => {
    const { loadModels } = useLLM.getState();
    for (const id of connectedKey.split("\n")) if (id) void loadModels(id);
  }, [connectedKey, catalogs]);

  const models = useMemo<Option[]>(() => connected.flatMap((p) => (catalogs[p.id]?.models ?? []).map((m): Option => ({
    value: `${p.id}/${m.id}`, label: m.name || m.id, provider: p.name, caps: m.caps, reasoning: m.reasoning ?? [], kind: "model",
  }))), [connected, catalogs]);
  const defaultModel = defaults?.chatModel ?? "";
  const defaultOption = models.find((o) => o.value === defaultModel);
  const options = useMemo<Option[]>(() => {
    const entry: Option = { value: "", label: "Use default", provider: "", caps: defaultOption?.caps ?? null, reasoning: defaultOption?.reasoning ?? [], kind: "default" };
    const stray: Option[] = selected && !models.some((o) => o.value === selected)
      ? [{ value: selected, label: selected, provider: "", caps: null, reasoning: [], kind: "missing" }] : [];
    return [entry, ...stray, ...models];
  }, [models, selected, defaultOption]);
  const current = options.find((o) => o.value === selected) ?? options[0]!;
  return { connected, options, current, defaultModel, loading: connected.some((p) => catalogs[p.id]?.loading ?? true), failed: connected.filter((p) => catalogs[p.id]?.error), catalogs };
}

/** Every selectable model across connected providers, the current choice and the catalogs' load state. */
export interface ModelOptions {
  connected: ProviderStatus[];
  options: Option[];
  current: Option;
  defaultModel: string;
  loading: boolean;
  failed: ProviderStatus[];
  catalogs: Record<string, ModelCatalog>;
}

/** The searchable model picker; "Use default" follows the default chat model in AI settings. */
export function ModelCombobox({ id, describedBy, models, onPick }: { id: string; describedBy?: string; models: ModelOptions; onPick: (option: Option | null) => void }) {
  const { options, current, defaultModel } = models;
  const defaultLabel = defaultModel ? (options.find((o) => o.value === defaultModel)?.label ?? defaultModel) : null;
  return (
    <Combobox<Option> items={options} value={current} onValueChange={onPick} itemToStringLabel={(o) => o.label} isItemEqualToValue={(a, b) => a.value === b.value} filter={matches}>
      <ComboboxInput id={id} aria-describedby={describedBy} placeholder="Search models…" />
      <ComboboxPopup>
        <ComboboxEmpty>No model matches your search.</ComboboxEmpty>
        <ComboboxList>
          {(option: Option) => (
            <ComboboxItem key={option.value || "default"} value={option}>
              <div className="flex min-w-0 flex-col gap-0.5">
                <span className="truncate">{option.kind === "default" ? (defaultLabel ? `Use default (${defaultLabel})` : "Use default") : option.label}</span>
                {option.kind === "model" && <span className="flex flex-wrap items-center gap-1 text-muted-foreground text-xs"><span>{option.provider}</span>{option.caps && <CapBadges caps={option.caps} />}</span>}
                {option.kind === "missing" && <Badge size="sm" variant="warning" className="self-start">Not offered by a connected provider</Badge>}
              </div>
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxPopup>
    </Combobox>
  );
}

/** The Persona tab's model settings: model, reasoning level and whether the bot is an executive assistant. */
export function ModelTab({ form, dispatch }: { form: FormState; dispatch: Dispatch<Action> }) {
  const models = useModelOptions(form.model);
  const { connected, current, loading, failed, catalogs } = models;
  const patch = (p: Partial<FormState>) => dispatch({ type: "patch", patch: p });
  const levels = current.reasoning;

  const choose = (option: Option | null) => {
    if (!option) return;
    patch({ model: option.value, reasoning: option.reasoning.includes(form.reasoning) ? form.reasoning : "" });
  };

  return (
    <div className="flex flex-col gap-4">
      {connected.length === 0 ? (
        <div className="flex items-center justify-between gap-2 text-sm">
          <span className="text-muted-foreground">No provider connected.</span>
          <Button size="sm" variant="outline" onClick={() => useUI.getState().openSettings("ai")}>Connect</Button>
        </div>
      ) : (
        <Block label="Model">
          {(id, describedBy) => <ModelCombobox id={id} describedBy={describedBy} models={models} onPick={choose} />}
        </Block>
      )}
      {current.kind === "missing" && <p role="status" className="text-warning-foreground text-xs">This model is no longer offered. Pick another.</p>}
      {loading && <p role="status" className="text-muted-foreground text-xs">Loading models…</p>}
      {failed.map((p) => (
        <p key={p.id} role="alert" className="flex items-center justify-between gap-2 text-destructive-foreground text-xs">
          <span className="selectable break-words">{p.name}: {catalogs[p.id]?.error}</span>
          <Button size="xs" variant="outline" onClick={() => void useLLM.getState().loadModels(p.id, true)}>Retry</Button>
        </p>
      ))}
      {levels.length > 0 && (
        <Block label="Reasoning">
          {(id) => (
            <Select value={form.reasoning} items={[{ value: "", label: "Default" }, ...levels.map((l) => ({ value: l, label: l }))]} onValueChange={(v) => { if (v !== null) patch({ reasoning: v }); }}>
              <SelectTrigger id={id} size="sm"><SelectValue /></SelectTrigger>
              <SelectPopup>
                <SelectGroup>
                  <SelectItem value="">Default</SelectItem>
                  {levels.map((l) => <SelectItem key={l} value={l}>{l}</SelectItem>)}
                </SelectGroup>
              </SelectPopup>
            </Select>
          )}
        </Block>
      )}
      <label className="flex items-center justify-between gap-3 text-sm">
        Executive assistant
        <Switch checked={form.executive} onCheckedChange={(executive) => patch({ executive })} />
      </label>
    </div>
  );
}
