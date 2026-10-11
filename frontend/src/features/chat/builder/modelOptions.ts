import { useEffect, useMemo } from "react";
import type { Caps, Model, ProviderStatus } from "@/mygo";
import { CURATED, matchCurated, type CuratedModel } from "@/lib/modelCatalog";
import { companyOf, isChatModel } from "@/lib/models";
import { useLLM, type ModelCatalog } from "@/stores/llm";

/** One selectable model across connected providers. The empty value is "use the defaults". */
export interface Option {
  /** `provider/model`, the string a bot stores */
  value: string;
  label: string;
  provider: string;
  /** the AI company that makes the model, which differs from the provider for aggregators */
  company: string;
  caps: Caps | null;
  reasoning: string[];
  /** set for handpicked models: what they are best at and how they score */
  curated: CuratedModel | null;
  kind: "default" | "model" | "missing";
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

const entry = (over: Partial<Option>): Option => ({ value: "", label: "", provider: "", company: "", caps: null, reasoning: [], curated: null, kind: "model", ...over });

/**
 * The models a bot can use. Each connected provider contributes its handpicked models (`lib/modelCatalog`), one per
 * model, preferring the shortest id (the alias over dated snapshots). A provider that offers none of them (local
 * runtimes, custom endpoints) contributes its chat models instead so it is never empty. The bot's current model and
 * the default chat model are always kept so an older choice stays visible. Embedding, image and audio models never appear.
 */
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

  const defaultModel = defaults?.chatModel ?? "";
  const models = useMemo<Option[]>(() => {
    const keep = new Set([selected, defaultModel]);
    const picks: Option[] = [];
    const rest: Option[] = [];
    for (const p of connected) {
      const chat = (catalogs[p.id]?.models ?? []).filter(isChatModel);
      const best = new Map<string, { curated: CuratedModel; model: Model }>();
      for (const model of chat) {
        const curated = matchCurated(model.id);
        const have = curated && best.get(curated.id);
        if (curated && (!have || model.id.length < have.model.id.length)) best.set(curated.id, { curated, model });
      }
      const option = (model: Model, curated: CuratedModel | null) => entry({
        value: `${p.id}/${model.id}`, label: curated?.name ?? (model.name || model.id), provider: p.name, company: curated?.company ?? companyOf(model),
        caps: model.caps, reasoning: model.reasoning ?? [], curated,
      });
      for (const { curated, model } of best.values()) picks.push(option(model, curated));
      // Nothing handpicked here: offer every chat model. Otherwise only keep the models already in use.
      const extra = best.size === 0 ? chat : chat.filter((m) => keep.has(`${p.id}/${m.id}`));
      for (const model of extra) rest.push(option(model, null));
    }
    // Handpicked models follow the list's ranking; one model offered by several providers stays in provider order.
    picks.sort((a, b) => CURATED.indexOf(a.curated!) - CURATED.indexOf(b.curated!));
    const seen = new Set(picks.map((o) => o.value));
    return [...picks, ...rest.filter((o) => !seen.has(o.value) && seen.add(o.value))];
  }, [connected, catalogs, selected, defaultModel]);

  const defaultOption = models.find((o) => o.value === defaultModel);
  const options = useMemo<Option[]>(() => {
    const first = entry({ label: "Use default", company: defaultOption?.company ?? "", caps: defaultOption?.caps ?? null, reasoning: defaultOption?.reasoning ?? [], kind: "default" });
    const stray = selected && !models.some((o) => o.value === selected) ? [entry({ value: selected, label: selected, kind: "missing" })] : [];
    return [first, ...stray, ...models];
  }, [models, selected, defaultOption]);
  const current = options.find((o) => o.value === selected) ?? options[0]!;
  return { connected, options, current, defaultModel, loading: connected.some((p) => catalogs[p.id]?.loading ?? true), failed: connected.filter((p) => catalogs[p.id]?.error), catalogs };
}
