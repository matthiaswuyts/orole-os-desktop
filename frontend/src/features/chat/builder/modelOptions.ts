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
  /** id of the provider that serves it, and whether that is an "account" (a signed-in CLI) or an API provider */
  providerId: string;
  providerKind: string;
  /** the AI company that makes the model, which differs from the provider for aggregators */
  company: string;
  caps: Caps | null;
  reasoning: string[];
  /** set for handpicked models: what they are best at and how they score */
  curated: CuratedModel | null;
  /** "none" is the placeholder shown while no model is chosen; it is never listed */
  kind: "model" | "missing" | "none";
  /** a handpicked model (or the only kind a provider has): shown under All; every other model shows once a provider is chosen */
  featured: boolean;
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

/** The company behind an account provider's models: the CLI's own aliases ("opus") say nothing about it. */
const ACCOUNT_COMPANY: Record<string, string> = { claude: "Anthropic", codex: "OpenAI", grok: "xAI" };

const entry = (over: Partial<Option>): Option => ({ value: "", label: "", provider: "", providerId: "", providerKind: "", company: "", caps: null, reasoning: [], curated: null, featured: false, kind: "model", ...over });

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
      const option = (model: Model, curated: CuratedModel | null, featured: boolean) => entry({
        value: `${p.id}/${model.id}`, label: curated?.name ?? (model.name || model.id), provider: p.name, providerId: p.id, providerKind: p.kind, company: curated?.company ?? ACCOUNT_COMPANY[p.id] ?? companyOf(model),
        caps: model.caps, reasoning: model.reasoning ?? [], curated, featured,
      });
      for (const { curated, model } of best.values()) picks.push(option(model, curated, true));
      // Every chat model is offered once this provider is chosen; under All only the handpicked ones (and those in use) show,
      // or all of them when nothing here is handpicked.
      for (const model of chat) rest.push(option(model, null, best.size === 0 || keep.has(`${p.id}/${model.id}`)));
    }
    // Handpicked models follow the list's ranking; one model offered by several providers stays in provider order.
    picks.sort((a, b) => CURATED.indexOf(a.curated!) - CURATED.indexOf(b.curated!));
    const seen = new Set(picks.map((o) => o.value));
    return [...picks, ...rest.filter((o) => !seen.has(o.value) && seen.add(o.value))];
  }, [connected, catalogs, selected, defaultModel]);

  const options = useMemo<Option[]>(() => {
    const stray = selected && !models.some((o) => o.value === selected) ? [entry({ value: selected, label: selected, kind: "missing" })] : [];
    return [...stray, ...models];
  }, [models, selected]);
  // A bot saved before every bot had an explicit model still runs on the default chat model: show that one.
  const current = options.find((o) => o.value === selected)
    ?? (selected === "" ? options.find((o) => o.value === defaultModel) : undefined)
    ?? entry({ label: "Choose a model", kind: "none" });
  return { connected, options, current, defaultModel, loading: connected.some((p) => catalogs[p.id]?.loading ?? true), failed: connected.filter((p) => catalogs[p.id]?.error), catalogs };
}
