import { useEffect, useId, useMemo, useState } from "react";
import type { Caps, Defaults } from "@/mygo";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Combobox, ComboboxEmpty, ComboboxInput, ComboboxItem, ComboboxList, ComboboxPopup } from "@/components/ui/combobox";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { useLLM } from "@/stores/llm";

/** One selectable model across all connected providers; `value` is the stored "provider/model" string. */
interface Option {
  value: string;
  label: string;
  /** model id as the provider knows it */
  model: string;
  provider: string;
  /** null when the stored selection is not offered by any connected provider */
  caps: Caps | null;
}

/** 128000 -> "128K", 1048576 -> "1M"; 0 means the provider did not say. */
function formatContext(tokens: number): string {
  if (tokens >= 1_000_000) return `${+(tokens / 1_000_000).toFixed(1)}M`;
  return `${Math.round(tokens / 1000)}K`;
}

const CAPABILITIES = [["tools", "Tools"], ["vision", "Vision"], ["pdf", "PDF"], ["reasoning", "Reasoning"]] as const;

/** Capability badges from the provider's catalogue; only capabilities the model has are listed. */
export function CapBadges({ caps }: { caps: Caps }) {
  return <>
    {CAPABILITIES.filter(([key]) => caps[key]).map(([key, label]) => <Badge key={key} size="sm" variant="outline">{label}</Badge>)}
    {caps.contextWindow > 0 && <Badge size="sm" variant="secondary">{formatContext(caps.contextWindow)} context</Badge>}
  </>;
}

function matches(option: Option, query: string): boolean {
  const haystack = `${option.label} ${option.model} ${option.provider}`.toLowerCase();
  return query.toLowerCase().split(/\s+/).every((token) => haystack.includes(token));
}

function ModelPicker({ label, hint, value, options, busy, needsTools, onChange }: {
  label: string; hint: string; value: string; options: Option[]; busy: boolean; needsTools?: boolean; onChange: (value: string) => void;
}) {
  const id = useId();
  // A stored selection that no connected provider offers stays visible (and flagged) instead of vanishing.
  const stray: Option | null = value && !options.some((o) => o.value === value)
    ? { value, label: value, model: value, provider: "", caps: null }
    : null;
  const items = stray ? [stray, ...options] : options;
  const selected = items.find((o) => o.value === value) ?? null;
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <label htmlFor={id} className="font-medium">{label}</label>
      <Combobox<Option>
        items={items}
        value={selected}
        onValueChange={(next) => { if (next && next.value !== value) onChange(next.value); }}
        itemToStringLabel={(option) => option.label}
        isItemEqualToValue={(a, b) => a.value === b.value}
        filter={matches}
        disabled={busy || options.length === 0}
      >
        <ComboboxInput id={id} placeholder={options.length === 0 ? "No models available" : "Search models…"} aria-describedby={`${id}-hint`} />
        <ComboboxPopup>
          <ComboboxEmpty>No model matches your search.</ComboboxEmpty>
          <ComboboxList>
            {(option: Option) => (
              <ComboboxItem key={option.value} value={option}>
                <div className="flex min-w-0 flex-col gap-0.5">
                  <span className="truncate">{option.label}</span>
                  <span className="flex flex-wrap items-center gap-1 text-xs text-muted-foreground">
                    {option.caps ? <><span>{option.provider}</span><CapBadges caps={option.caps} /></> : <Badge size="sm" variant="warning">Not offered by a connected provider</Badge>}
                  </span>
                </div>
              </ComboboxItem>
            )}
          </ComboboxList>
        </ComboboxPopup>
      </Combobox>
      <p id={`${id}-hint`} className="text-xs text-muted-foreground">{hint}</p>
      {selected?.caps && (
        <p className="flex flex-wrap items-center gap-1" aria-label={`${label} capabilities`}>
          <CapBadges caps={selected.caps} />
        </p>
      )}
      {selected && !selected.caps && <p role="status" className="text-xs text-warning-foreground">This model is not offered by a connected provider. Pick another or reconnect its provider.</p>}
      {needsTools && selected?.caps && !selected.caps.tools && (
        <p role="status" className="text-xs text-warning-foreground">This model cannot call tools, so bots using it cannot search the web or use memory.</p>
      )}
    </div>
  );
}

/** Default chat and memory models, chosen from the models of connected providers. */
export function ModelDefaults() {
  const providers = useLLM((state) => state.providers);
  const defaults = useLLM((state) => state.defaults);
  const catalogs = useLLM((state) => state.models);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const connected = useMemo(() => providers.filter((p) => p.connected), [providers]);
  const connectedKey = connected.map((p) => p.id).join("\n");

  // Catalogues load lazily; a dropped entry (provider changed) is fetched again here.
  useEffect(() => {
    const { loadModels } = useLLM.getState();
    for (const id of connectedKey.split("\n")) if (id) void loadModels(id);
  }, [connectedKey, catalogs]);

  const options = useMemo<Option[]>(() => connected.flatMap((p) =>
    (catalogs[p.id]?.models ?? []).map((m) => ({ value: `${p.id}/${m.id}`, label: m.name || m.id, model: m.id, provider: p.name, caps: m.caps }))), [connected, catalogs]);
  const loading = connected.some((p) => catalogs[p.id]?.loading ?? true);
  const failed = connected.filter((p) => catalogs[p.id]?.error);

  async function choose(field: keyof Defaults, value: string) {
    setError(null);
    setSaving(true);
    const result = await useLLM.getState().setDefaults({ chatModel: "", memoryModel: "", ...defaults, [field]: value });
    setSaving(false);
    if (!result.ok) setError(result.error);
  }

  return (
    <section aria-labelledby="ai-defaults-title" className="flex min-w-0 flex-col gap-2">
      <h2 id="ai-defaults-title" className="text-sm font-medium">Default models</h2>
      {connected.length === 0 ? (
        <Empty className="rounded-lg border bg-card">
          <EmptyHeader>
            <EmptyTitle>Connect a provider to choose models</EmptyTitle>
            <EmptyDescription>Default models come from the providers you connect above.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className="flex flex-col gap-4 rounded-lg border bg-card p-3">
          <p className="text-muted-foreground">Bots use these unless a bot picks its own model.</p>
          <ModelPicker label="Default chat model" hint="Used for conversations with your bots." value={defaults?.chatModel ?? ""} options={options} busy={saving} needsTools onChange={(value) => void choose("chatModel", value)} />
          <ModelPicker label="Memory model" hint="A cheap model for summaries and memory. It runs in the background, so cost matters more than quality." value={defaults?.memoryModel ?? ""} options={options} busy={saving} onChange={(value) => void choose("memoryModel", value)} />
          {loading && <p role="status" className="text-xs text-muted-foreground">Loading models…</p>}
          {failed.map((p) => (
            <Alert key={p.id} variant="error">
              <AlertTitle>Could not load {p.name} models</AlertTitle>
              <AlertDescription className="selectable break-words">{catalogs[p.id]?.error}</AlertDescription>
              <Button size="sm" variant="outline" className="self-start" onClick={() => void useLLM.getState().loadModels(p.id, true)}>Try again</Button>
            </Alert>
          ))}
          {error && <p role="alert" className="text-destructive-foreground text-sm">Could not save: {error}</p>}
        </div>
      )}
    </section>
  );
}
