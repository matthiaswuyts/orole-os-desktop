import { ProviderLogo } from "@/components/ProviderLogo";
import { cn } from "@/lib/utils";

export interface ProviderChoice {
  id: string;
  name: string;
  /** "account" = the user's own plan through a vendor CLI; anything else is an API provider */
  kind: string;
  /** models it serves */
  count: number;
}

const ALL = "all";

/**
 * Where should the model run? One tile per connected provider, an account (Claude, ChatGPT, Grok: your plan) or an API
 * provider (OpenRouter, OpenAI: pay per use), so the choice between them is the first thing the picker asks.
 */
export function ProviderTabs({ choices, value, onChange, total }: { choices: readonly ProviderChoice[]; value: string; onChange: (id: string) => void; total: number }) {
  return (
    <div role="radiogroup" aria-label="Provider" className="flex flex-wrap gap-1.5 px-2 pb-2">
      <Tile label="All" caption={`${total} models`} selected={value === ALL} onSelect={() => onChange(ALL)} />
      {choices.map((c) => (
        <Tile key={c.id} label={c.name} caption={c.kind === "account" ? "Your account" : "API key"} selected={value === c.id} onSelect={() => onChange(c.id)}
          logo={<ProviderLogo id={c.id} name={c.name} className="size-7 rounded-md" />} />
      ))}
    </div>
  );
}

function Tile({ label, caption, selected, onSelect, logo }: { label: string; caption: string; selected: boolean; onSelect: () => void; logo?: React.ReactNode }) {
  return (
    <button type="button" role="radio" aria-checked={selected} onClick={onSelect}
      className={cn("flex min-w-0 items-center gap-2 rounded-xl border px-2.5 py-1.5 text-left outline-none transition-colors hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring",
        selected ? "border-primary/50 bg-accent" : "bg-background/40")}>
      {logo}
      <span className="flex min-w-0 flex-col leading-tight">
        <span className={cn("truncate text-sm", selected ? "font-semibold" : "font-medium")}>{label}</span>
        <span className="truncate text-muted-foreground text-xs">{caption}</span>
      </span>
    </button>
  );
}
