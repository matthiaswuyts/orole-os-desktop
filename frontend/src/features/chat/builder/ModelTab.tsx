import { useEffect, useMemo, type Dispatch } from "react";
import type { Caps } from "@/mygo";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Combobox, ComboboxEmpty, ComboboxInput, ComboboxItem, ComboboxList, ComboboxPopup } from "@/components/ui/combobox";
import { Input } from "@/components/ui/input";
import { Select, SelectGroup, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { CapBadges } from "@/features/settings/ModelDefaults";
import { useUI } from "@/stores/ui";
import { useLLM } from "@/stores/llm";
import { LIMITS, type Action, type FormState, type Problems } from "./botForm";
import { Block, Section } from "./controls";

/** One selectable model across connected providers. The empty value is "use the defaults". */
interface Option {
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
export function useModelOptions(selected: string) {
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

export function ModelTab({ form, dispatch, problems }: { form: FormState; dispatch: Dispatch<Action>; problems: Problems }) {
  const { connected, options, current, defaultModel, loading, failed, catalogs } = useModelOptions(form.model);
  const patch = (p: Partial<FormState>) => dispatch({ type: "patch", patch: p });
  const levels = current.reasoning;
  const defaultLabel = defaultModel ? (options.find((o) => o.value === defaultModel)?.label ?? defaultModel) : null;

  const choose = (option: Option | null) => {
    if (!option) return;
    patch({ model: option.value, reasoning: option.reasoning.includes(form.reasoning) ? form.reasoning : "" });
  };

  return (
    <div className="flex flex-col gap-3">
      {connected.length === 0 && (
        <Alert variant="warning">
          <AlertTitle>No provider is connected</AlertTitle>
          <AlertDescription>
            <p>Bots call a model provider directly. Connect one to choose a model and to try this bot in the preview.</p>
            <Button size="sm" className="mt-2" onClick={() => useUI.getState().openSettings("ai")}>Connect a provider</Button>
          </AlertDescription>
        </Alert>
      )}
      {connected.length > 0 && <Section title="Model" description="Which model answers this bot. “Use default” follows the default chat model in AI settings.">
        <Block label="Model" hint={defaultLabel ? `Default chat model: ${defaultLabel}.` : "No default chat model is set yet; choose one here or in AI settings."}>
          {(id, describedBy) => (
            <Combobox<Option> items={options} value={current} onValueChange={choose} itemToStringLabel={(o) => o.label} isItemEqualToValue={(a, b) => a.value === b.value} filter={matches}>
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
          )}
        </Block>
        {current.kind === "missing" && <p role="status" className="text-warning-foreground text-xs">This bot’s model is not offered by a connected provider. Pick another model or reconnect the provider.</p>}
        {current.caps && !current.caps.tools && <p role="status" className="text-warning-foreground text-xs">This model cannot call tools, so the bot cannot search the web or use memory.</p>}
        {loading && <p role="status" className="text-muted-foreground text-xs">Loading models…</p>}
        {failed.map((p) => (
          <Alert key={p.id} variant="error">
            <AlertTitle>Could not load {p.name} models</AlertTitle>
            <AlertDescription className="selectable break-words">{catalogs[p.id]?.error}</AlertDescription>
            <Button size="sm" variant="outline" className="self-start" onClick={() => void useLLM.getState().loadModels(p.id, true)}>Try again</Button>
          </Alert>
        ))}
        {levels.length > 0 && (
          <Block label="Reasoning level" hint="How much the model thinks before answering. Higher levels are slower and can cost more.">
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
      </Section>}

      <Section title="Limits" description="Bound what one answer may do. Leave empty for the defaults.">
        <div className="grid gap-3 sm:grid-cols-2">
          <Block label="Max steps" error={problems.maxSteps} hint={`Model calls per answer. Default ${LIMITS.steps.default}, up to ${LIMITS.steps.max}.`}>
            {(id, describedBy) => (
              <Input nativeInput id={id} type="number" inputMode="numeric" min={LIMITS.steps.min} max={LIMITS.steps.max} step={1} value={form.maxSteps} placeholder={String(LIMITS.steps.default)} aria-invalid={!!problems.maxSteps || undefined} aria-describedby={describedBy} onChange={(e) => patch({ maxSteps: e.target.value })} />
            )}
          </Block>
          <Block label="Max tool calls" error={problems.maxToolCalls} hint={`Tool uses per answer. Default ${LIMITS.toolCalls.default}, up to ${LIMITS.toolCalls.max}.`}>
            {(id, describedBy) => (
              <Input nativeInput id={id} type="number" inputMode="numeric" min={LIMITS.toolCalls.min} max={LIMITS.toolCalls.max} step={1} value={form.maxToolCalls} placeholder={String(LIMITS.toolCalls.default)} aria-invalid={!!problems.maxToolCalls || undefined} aria-describedby={describedBy} onChange={(e) => patch({ maxToolCalls: e.target.value })} />
            )}
          </Block>
        </div>
      </Section>

      <Section title="Memory writes">
        <div className="flex items-start justify-between gap-3">
          <div className="flex min-w-0 flex-col gap-0.5">
            <label htmlFor="bot-memory-approval" className="font-medium text-sm">Hold memory writes for my approval</label>
            <p className="text-muted-foreground text-xs">When on, what the bot wants to remember waits in the Memory tab until you approve it. When off, the bot saves memories right away and you can still edit or delete them.</p>
          </div>
          <Switch id="bot-memory-approval" checked={form.memoryApproval} onCheckedChange={(checked) => patch({ memoryApproval: checked })} />
        </div>
      </Section>
    </div>
  );
}
