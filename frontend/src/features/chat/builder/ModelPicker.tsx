import { useId, useMemo, useRef, useState } from "react";
import { BrainIcon, ChevronsUpDownIcon, CoinsIcon, EyeIcon, FileTextIcon, FunnelIcon, SearchIcon, TrophyIcon, WrenchIcon, ZapIcon, type LucideIcon } from "lucide-react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { CompanyLogo } from "@/components/CompanyLogo";
import { ProviderLogo } from "@/components/ProviderLogo";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Menu, MenuCheckboxItem, MenuGroup, MenuGroupLabel, MenuItem, MenuPopup, MenuSeparator, MenuTrigger } from "@/components/ui/menu";
import { Popover, PopoverPopup, PopoverTrigger } from "@/components/ui/popover";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { STRENGTH_LABEL, type Strength } from "@/lib/modelCatalog";
import { cn } from "@/lib/utils";
import { useUI } from "@/stores/ui";
import type { ModelOptions, Option } from "./modelOptions";
import { ProviderTabs, type ProviderChoice } from "./ProviderTabs";

const ROW = 60;
const ALL = "all";

/** What each strength looks like in the filter menu and next to a model's name. */
const STRENGTHS: { id: Strength; icon: LucideIcon; tone: string }[] = [
  { id: "performance", icon: TrophyIcon, tone: "bg-amber-500/15 text-amber-400" },
  { id: "value", icon: CoinsIcon, tone: "bg-emerald-500/15 text-emerald-400" },
  { id: "speed", icon: ZapIcon, tone: "bg-yellow-500/15 text-yellow-300" },
];

/** Capabilities the provider reports for a model, shown as small coloured icons. */
const CAPABILITIES: { key: "vision" | "reasoning" | "tools" | "pdf"; label: string; icon: LucideIcon; tone: string }[] = [
  { key: "vision", label: "Vision", icon: EyeIcon, tone: "text-teal-400" },
  { key: "reasoning", label: "Reasoning", icon: BrainIcon, tone: "text-violet-400" },
  { key: "tools", label: "Tool calling", icon: WrenchIcon, tone: "text-orange-400" },
  { key: "pdf", label: "PDF comprehension", icon: FileTextIcon, tone: "text-indigo-400" },
];

const matches = (option: Option, query: string) => {
  const haystack = `${option.label} ${option.value} ${option.provider} ${option.company} ${option.curated?.blurb ?? ""}`.toLowerCase();
  return query.toLowerCase().split(/\s+/).every((token) => haystack.includes(token));
};

/** An icon with a tooltip naming it; the name is also read out to screen readers. */
function IconTip({ label, className, children }: { label: string; className?: string; children: React.ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger render={<span className={className} />}>{children}<span className="sr-only">{label}</span></TooltipTrigger>
      <TooltipPopup>{label}</TooltipPopup>
    </Tooltip>
  );
}

function Row({ option, label, selected, active, id, showProvider, onPick, style }: {
  option: Option; label: string; selected: boolean; active: boolean; id: string; showProvider: boolean; onPick: () => void; style: React.CSSProperties;
}) {
  const strengths = STRENGTHS.filter((s) => option.curated?.strengths.includes(s.id));
  const caps = option.caps ? CAPABILITIES.filter((c) => option.caps![c.key]) : [];
  const note = option.kind === "missing" ? "Not offered by a connected provider"
      : (option.curated?.blurb ?? option.company) + (showProvider ? ` · via ${option.provider}` : "");
  return (
    <div id={id} role="option" aria-selected={selected} onClick={onPick} style={style}
      className={cn("absolute inset-x-0 flex cursor-pointer items-center gap-3 rounded-xl px-3 hover:bg-accent", active && "ring-1 ring-ring/50")}>
      <CompanyLogo company={option.company} className={cn("size-5", !selected && "text-muted-foreground")} />
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="flex min-w-0 items-center gap-1.5">
          <span className={cn("truncate text-[15px]", selected ? "font-semibold" : "font-medium")}>{label}</span>
          {strengths.map(({ id: s, icon: Icon, tone }) => (
            <IconTip key={s} label={STRENGTH_LABEL[s]} className={cn("grid size-5 shrink-0 place-items-center rounded-full", tone)}><Icon aria-hidden className="size-3" /></IconTip>
          ))}
        </span>
        <span className="truncate text-muted-foreground text-xs">{note}</span>
      </span>
      {(caps.length > 0 || option.curated?.speed) && (
        <span className="flex shrink-0 flex-col items-end gap-1">
          <span className="flex items-center gap-1.5">
            {caps.map(({ key, label: capLabel, icon: Icon, tone }) => <IconTip key={key} label={capLabel}><Icon aria-hidden className={cn("size-3.5", tone)} /></IconTip>)}
          </span>
          {option.curated?.speed ? <span className="text-muted-foreground text-xs tabular-nums">{option.curated.speed} tok/s</span> : null}
        </span>
      )}
    </div>
  );
}

/** The funnel button and its menu: filters by what a model is best at. */
function FilterMenu({ strengths, onStrengths, active, onClear }: {
  strengths: Strength[]; onStrengths: (next: Strength[]) => void; active: boolean; onClear: () => void;
}) {
  return (
    <Menu>
      <MenuTrigger render={<Button variant="outline" size="icon" aria-label="Filters" className="relative shrink-0" />}>
        <FunnelIcon aria-hidden />
        {active && <span aria-hidden className="absolute end-1 top-1 size-1.5 rounded-full bg-primary" />}
      </MenuTrigger>
      <MenuPopup align="end" className="w-60">
        <MenuGroup>
          <MenuGroupLabel>Best at</MenuGroupLabel>
          {STRENGTHS.map(({ id, icon: Icon, tone }) => (
            <MenuCheckboxItem key={id} checked={strengths.includes(id)} closeOnClick={false} className="min-h-9"
              onCheckedChange={(on) => onStrengths(on ? [...strengths, id] : strengths.filter((s) => s !== id))}>
              <span className="flex items-center gap-2.5">
                <span className={cn("grid size-6 place-items-center rounded-full", tone)}><Icon aria-hidden className="size-3.5" /></span>
                {STRENGTH_LABEL[id]}
              </span>
            </MenuCheckboxItem>
          ))}
        </MenuGroup>
        {active && <>
          <MenuSeparator />
          <MenuItem onClick={onClear}>Clear filters</MenuItem>
        </>}
      </MenuPopup>
    </Menu>
  );
}

/** One company in the chip row; pressing the pressed one clears the filter. */
function CompanyChip({ label, pressed, onClick, logo }: { label: string; pressed: boolean; onClick: () => void; logo?: React.ReactNode }) {
  return (
    <button type="button" aria-pressed={pressed} onClick={onClick}
      className={cn("flex shrink-0 items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs outline-none transition-colors hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring", pressed ? "border-primary/50 bg-accent text-foreground" : "text-muted-foreground")}>
      {logo}{label}
    </button>
  );
}

/** The popup body. It mounts only while open, so its search, filters and virtual list start fresh every time. */
function Panel({ models, labelOf, onPick }: { models: ModelOptions; labelOf: (o: Option) => string; onPick: (option: Option) => void }) {
  const { options, current } = models;
  const listId = useId();
  const scroller = useRef<HTMLDivElement>(null);
  const [query, setQuery] = useState("");
  const [provider, setProvider] = useState(ALL);
  const [company, setCompany] = useState("");
  const [strengths, setStrengths] = useState<Strength[]>([]);
  const [active, setActive] = useState(0);
  // The highlight ring marks the keyboard's position only: without it the first row looks selected for no reason.
  const [keyboard, setKeyboard] = useState(false);

  const real = useMemo(() => options.filter((o) => o.kind === "model"), [options]);
  const choices = useMemo<ProviderChoice[]>(() => models.connected.map((p) => ({ id: p.id, name: p.name, kind: p.kind, count: real.filter((o) => o.providerId === p.id).length })).filter((c) => c.count > 0), [models.connected, real]);
  // A single-vendor account (Claude, ChatGPT, Grok) serves only its own models, so a company filter would be noise there:
  // it is offered for All and for aggregators such as OpenRouter.
  const accountOnly = provider !== ALL && models.connected.find((p) => p.id === provider)?.kind === "account";
  const companies = useMemo(() => [...new Set(real.filter((o) => (provider === ALL ? o.featured : o.providerId === provider)).map((o) => o.company))], [real, provider]);
  const showCompanies = !accountOnly && companies.length > 1;
  const menuActive = strengths.length > 0;
  const filtering = menuActive || provider !== ALL || query !== "" || company !== "";

  // A stray current choice only answers the search; any other filter narrows to real models.
  const shown = useMemo(() => options.filter((o) => o.kind !== "model"
    ? !menuActive && provider === ALL && company === "" && matches(o, query)
    : (provider === ALL ? o.featured || query !== "" : o.providerId === provider) && (company === "" || o.company === company)
      && (strengths.length === 0 || strengths.some((s) => o.curated?.strengths.includes(s))) && matches(o, query)),
  [options, query, provider, company, strengths, menuActive]);

  const virtual = useVirtualizer({ count: shown.length, getScrollElement: () => scroller.current, estimateSize: () => ROW, overscan: 6, initialRect: { width: 420, height: 300 } });
  const clamped = Math.min(active, Math.max(0, shown.length - 1));
  const refilter = (apply: () => void) => { apply(); setActive(0); setKeyboard(false); scroller.current?.scrollTo({ top: 0 }); };
  const clear = () => refilter(() => { setQuery(""); setProvider(ALL); setCompany(""); setStrengths([]); });

  const onKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === "Enter") { event.preventDefault(); const o = shown[clamped]; if (o) onPick(o); return; }
    const step = event.key === "ArrowDown" ? 1 : event.key === "ArrowUp" ? -1 : 0;
    if (!step) return;
    event.preventDefault();
    const next = Math.max(0, Math.min(shown.length - 1, clamped + step));
    setKeyboard(true);
    setActive(next);
    virtual.scrollToIndex(next);
  };

  return (
    <div className="flex min-w-0 flex-col">
      <div className="flex items-center gap-2 p-2">
        <div className="relative min-w-0 flex-1">
          <SearchIcon aria-hidden className="pointer-events-none absolute start-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input autoFocus role="combobox" aria-expanded aria-controls={listId} aria-activedescendant={shown[clamped] ? `${listId}-${clamped}` : undefined}
            value={query} onChange={(e) => refilter(() => setQuery(e.target.value))} onKeyDown={onKeyDown}
            placeholder="Search models…" aria-label="Search models" className="ps-9" />
        </div>
        <FilterMenu strengths={strengths} onStrengths={(next) => refilter(() => setStrengths(next))} active={menuActive} onClear={clear} />
      </div>
      {choices.length > 1 && <ProviderTabs choices={choices} value={provider} total={real.filter((o) => o.featured).length} onChange={(id) => refilter(() => { setProvider(id); setCompany(""); })} />}
      {showCompanies && (
        <div role="group" aria-label="Filter by company" className="flex gap-1.5 overflow-x-auto px-2 pb-2 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
          <CompanyChip label="All companies" pressed={company === ""} onClick={() => refilter(() => setCompany(""))} />
          {companies.map((name) => (
            <CompanyChip key={name} label={name} pressed={company === name} onClick={() => refilter(() => setCompany(company === name ? "" : name))} logo={<CompanyLogo company={name} className="size-3.5" />} />
          ))}
        </div>
      )}

      <div className="flex min-h-0 px-1.5 pb-1.5">
        <div ref={scroller} id={listId} role="listbox" aria-label="Models" className="h-75 min-w-0 flex-1 overflow-y-auto">
          {shown.length === 0 ? (
            <div role="status" className="flex h-full flex-col items-center justify-center gap-2 text-center text-muted-foreground text-sm">
              No model matches.
              {filtering && <Button size="xs" variant="outline" onClick={clear}>Clear filters</Button>}
            </div>
          ) : (
            <div className="relative w-full" style={{ height: virtual.getTotalSize() }}>
              {virtual.getVirtualItems().map((row) => {
                const option = shown[row.index]!;
                return <Row key={option.value} id={`${listId}-${row.index}`} option={option} label={labelOf(option)} selected={option.value === current.value}
                  active={keyboard && row.index === clamped} showProvider={choices.length > 1} onPick={() => onPick(option)} style={{ height: row.size, transform: `translateY(${row.start}px)` }} />;
              })}
            </div>
          )}
        </div>
      </div>
      <button type="button" onClick={() => useUI.getState().openSettings("ai")} className="border-t px-3 py-2 text-start text-muted-foreground text-xs outline-none hover:bg-accent hover:text-foreground focus-visible:bg-accent">
        Use a Claude, ChatGPT or Grok account, or add an API key, in Settings &gt; AI providers
      </button>
    </div>
  );
}

/**
 * The model picker: a popover over the handpicked models (see `lib/modelCatalog`) with search, a provider chooser, a company
 * filter (only for All and aggregators; an account serves only its own models) and a filter menu for what each model is best at. The list is virtualized, so
 * even a provider that falls back to its whole catalogue stays fast.
 */
export function ModelPicker({ id, describedBy, models, onPick, compact }: { id: string; describedBy?: string; models: ModelOptions; onPick: (option: Option | null) => void; /** a one-line trigger for a settings row */ compact?: boolean }) {
  const { current } = models;
  const [open, setOpen] = useState(false);
  const labelOf = (o: Option) => o.label;
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger render={<Button id={id} aria-describedby={describedBy} variant={compact ? "ghost" : "outline"} className={compact ? "h-7 min-w-0 max-w-full justify-end gap-1.5 px-1.5 font-normal text-muted-foreground sm:h-7" : "h-auto min-h-14 w-full justify-between gap-3 px-3.5 py-2.5 font-normal sm:h-auto"} />}>
        <span className="flex min-w-0 items-center gap-3">
          {current.kind === "model" && current.providerId
            ? <ProviderLogo id={current.providerId} name={current.provider} className={compact ? "size-5 rounded-md" : "size-9"} />
            : <CompanyLogo company={current.company} className={compact ? "size-4" : "size-6"} />}
          <span className="flex min-w-0 flex-col items-start gap-0.5 text-left leading-snug">
            <span className={compact ? "w-full truncate text-sm" : "w-full truncate text-[15px] font-medium"}>{labelOf(current)}</span>
            {!compact && current.kind === "model" && <span className="w-full truncate text-muted-foreground text-xs">via {current.provider}{current.providerKind === "account" ? " account" : ""}</span>}
          </span>
        </span>
        <ChevronsUpDownIcon aria-hidden className="size-4 shrink-0 opacity-60" />
      </PopoverTrigger>
      <PopoverPopup align="end" className="w-[30rem] max-w-[calc(100vw-2rem)] [&_[data-slot=popover-viewport]]:p-0">
        <Panel models={models} labelOf={labelOf} onPick={(option) => { onPick(option); setOpen(false); }} />
      </PopoverPopup>
    </Popover>
  );
}
