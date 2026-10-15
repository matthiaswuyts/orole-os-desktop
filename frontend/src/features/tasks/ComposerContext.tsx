import { useState } from "react";
import type { ContextBreakdown, ContextCategory, ContextUsage } from "@/mygo";
import { ChevronRightIcon, CircleHelpIcon, CpuIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsiblePanel, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Popover, PopoverPopup, PopoverTitle, PopoverTrigger } from "@/components/ui/popover";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

/** Unknown counts stay unknown; billing totals are never used as context occupancy. */
export function contextUsage(usage?: ContextUsage | null) {
  const count = (value?: number | null) => typeof value === "number" && Number.isFinite(value) && value >= 0 ? value : null;
  const used = count(usage?.contextTokens);
  const limit = count(usage?.contextWindow);
  const fraction = used !== null && limit !== null && limit > 0 ? used / limit : null;
  return { used, limit, fraction };
}

const GRID_COLUMNS = 20;
const GRID_CELLS = GRID_COLUMNS * 5;
/** Theme chart tokens, so every theme recolours the breakdown; past five categories they repeat at half strength. */
const PALETTE = ["var(--chart-1)", "var(--chart-2)", "var(--chart-3)", "var(--chart-4)", "var(--chart-5)"];

export type ContextCell = { kind: "used" | "buffer" | "free"; category?: string };

/**
 * Lays the harness's categories over a fixed grid of the window. Each used category with tokens
 * keeps at least one cell, so small but real contributors stay visible; shares are distributed by
 * largest remainder so the cells add up to the reported occupancy.
 */
export function contextCells(breakdown: ContextBreakdown, cells = GRID_CELLS): ContextCell[] {
  const window = breakdown.windowTokens;
  if (!(window > 0)) return [];
  const used = breakdown.categories.filter((c) => c.kind === "used" && c.tokens > 0);
  const usedTokens = used.reduce((sum, c) => sum + c.tokens, 0);
  const target = Math.min(cells, Math.max(Math.min(used.length, cells), Math.round(usedTokens / window * cells)));
  const shares = used.map((c) => c.tokens / window * cells);
  const counts = shares.map((share) => Math.max(1, Math.floor(share)));
  let total = counts.reduce((sum, n) => sum + n, 0);
  const byRemainder = shares.map((share, index) => ({ index, rest: share - Math.floor(share) })).sort((a, b) => b.rest - a.rest);
  for (let i = 0; total < target && byRemainder.length; i = (i + 1) % byRemainder.length, total++) counts[byRemainder[i].index]++;
  // Over-full windows: trim the largest categories first, never below one cell.
  while (total > target) {
    const largest = counts.reduce((best, n, index) => n > counts[best] ? index : best, 0);
    if (counts[largest] <= 1) break;
    counts[largest]--;
    total--;
  }
  const grid: ContextCell[] = used.flatMap((c, index) => Array.from({ length: counts[index] }, () => ({ kind: "used" as const, category: c.name })));
  const buffer = breakdown.categories.filter((c) => c.kind === "buffer").reduce((sum, c) => sum + c.tokens, 0);
  const bufferCells = Math.min(cells - grid.length, Math.round(buffer / window * cells));
  for (let i = 0; i < bufferCells; i++) grid.push({ kind: "buffer" });
  while (grid.length < cells) grid.push({ kind: "free" });
  return grid.slice(0, cells);
}

const formatTokens = (value: number) => new Intl.NumberFormat("en", { notation: "compact", maximumFractionDigits: 1 }).format(value);

export function ComposerContext({ usage }: { usage?: ContextUsage | null }) {
  const { fraction } = contextUsage(usage);
  const breakdown = usage?.breakdown && usage.breakdown.windowTokens > 0 ? usage.breakdown : null;
  const percentage = fraction === null ? null : Math.round(fraction * 100);
  const label = percentage === null ? "Context usage: not fully reported" : `Context usage: ${percentage}%`;
  const warn = fraction !== null && fraction >= 0.85;
  return <Popover>
    <PopoverTrigger render={<Button type="button" variant="ghost" size="sm" className="h-7 gap-1.5 rounded-full px-2 text-muted-foreground tabular-nums" aria-label={label} />}>
      {fraction === null ? <CircleHelpIcon aria-hidden data-icon="inline-start" /> : <svg aria-hidden viewBox="0 0 24 24" className={cn("size-4", warn && "text-warning")}>
        <circle cx="12" cy="12" r="9" fill="none" stroke="currentColor" strokeWidth="2" opacity="0.2" />
        <circle cx="12" cy="12" r="9" fill="none" stroke="currentColor" strokeWidth="2" pathLength="1" strokeDasharray="1" strokeDashoffset={1 - Math.min(1, fraction)} transform="rotate(-90 12 12)" />
      </svg>}
      {percentage === null ? "Context" : `${percentage}%`}
    </PopoverTrigger>
    <PopoverPopup side="top" align="end" sideOffset={8} className="w-84 max-w-[calc(100vw-2rem)]">
      <div className="flex flex-col gap-4">
        <div className="flex min-w-0 items-center gap-2.5">
          <span aria-hidden className="flex size-7 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary"><CpuIcon className="size-4" /></span>
          <PopoverTitle className="flex-1">Context window</PopoverTitle>
          {breakdown?.model && <span className="min-w-0 truncate text-xs text-muted-foreground">{breakdown.model}</span>}
        </div>
        <ContextWindow usage={usage} bounded />
      </div>
    </PopoverPopup>
  </Popover>;
}

/**
 * What the model holds: occupancy against the window, then the harness's own per-category grid and rows, or a plain meter
 * when it reports totals only. Shared by the composer's popover (`bounded`: the rows scroll inside it) and the task details
 * rail, which scrolls as a whole.
 */
export function ContextWindow({ usage, bounded = false }: { usage?: ContextUsage | null; bounded?: boolean }) {
  const { used, limit, fraction } = contextUsage(usage);
  const breakdown = usage?.breakdown && usage.breakdown.windowTokens > 0 ? usage.breakdown : null;
  const percentage = fraction === null ? null : Math.round(fraction * 100);
  const warn = fraction !== null && fraction >= 0.85;
  return <>
    <div className="flex items-baseline justify-between gap-4 tabular-nums">
      <p className="flex items-baseline gap-1.5">
        <span className="text-2xl font-semibold tracking-tight">{used === null ? "Not reported" : formatTokens(used)}</span>
        {limit !== null && limit > 0 && <span className="text-sm text-muted-foreground">/ {formatTokens(limit)}</span>}
      </p>
      {percentage !== null && <span className={cn("text-sm text-muted-foreground", warn && "font-medium text-warning-foreground")}>{percentage}%</span>}
    </div>
    {breakdown ? <Breakdown breakdown={breakdown} used={used} bounded={bounded} />
      : <TotalsOnly usage={usage} fraction={fraction} percentage={percentage} />}
  </>;
}

function Breakdown({ breakdown, used, bounded }: { breakdown: ContextBreakdown; used: number | null; bounded: boolean }) {
  const cells = contextCells(breakdown);
  const usedCategories = breakdown.categories.filter((c) => c.kind === "used" && c.tokens > 0);
  const colors = new Map(usedCategories.map((c, index) => {
    const base = PALETTE[index % PALETTE.length];
    return [c.name, index < PALETTE.length ? base : `color-mix(in oklab, ${base} 50%, transparent)`];
  }));
  const buffer = breakdown.categories.filter((c) => c.kind === "buffer" && c.tokens > 0);
  const deferred = breakdown.categories.filter((c) => c.kind === "deferred" && c.tokens > 0);
  const left = Math.max(0, breakdown.windowTokens - breakdown.usedTokens);
  const percentage = Math.round(breakdown.usedTokens / breakdown.windowTokens * 100);
  // Live occupancy moves on every model request; the breakdown is refreshed when a turn ends.
  const stale = used !== null && used !== breakdown.usedTokens;
  return <>
    <div role="img" aria-label={`${percentage}% of the context window used`} className="grid gap-1 rounded-lg bg-muted p-2.5" style={{ gridTemplateColumns: `repeat(${GRID_COLUMNS}, minmax(0, 1fr))` }}>
      {cells.map((cell, index) => <span key={index} className={cn("aspect-square rounded-[3px]", cell.kind === "free" && "bg-foreground/8", cell.kind === "buffer" && "bg-foreground/20")}
        style={cell.kind === "used" ? { backgroundColor: colors.get(cell.category ?? "") } : undefined} />)}
    </div>
    <ul aria-label="Context breakdown" className={cn("-mx-1 flex flex-col", bounded && "max-h-72 overflow-y-auto")}>
      {usedCategories.map((c) => <CategoryRow key={c.name} category={c} color={colors.get(c.name)} windowTokens={breakdown.windowTokens} />)}
      {buffer.map((c) => <CategoryRow key={c.name} category={c} swatch="bg-foreground/20" windowTokens={breakdown.windowTokens} />)}
      {deferred.length > 0 && <li aria-hidden className="px-1 pt-2 pb-1 text-xs text-muted-foreground">Loaded on demand, not in context</li>}
      {deferred.map((c) => <CategoryRow key={c.name} category={c} swatch="border border-dashed border-muted-foreground/50" muted />)}
    </ul>
    <div className="flex flex-col gap-1 border-t pt-3 text-xs text-muted-foreground">
      <div className="flex items-center justify-between gap-4 tabular-nums">
        <span>{formatTokens(left)} left</span>
        {breakdown.autoCompactTokens != null && <span>Auto-compacts at {formatTokens(breakdown.autoCompactTokens)}</span>}
      </div>
      {stale && <p>Breakdown as of the last finished turn ({formatTokens(breakdown.usedTokens)} tokens).</p>}
    </div>
  </>;
}

/** `windowTokens` adds the category's share of the window; on-demand rows sit outside it and get none. */
function CategoryRow({ category, color, swatch, muted = false, windowTokens }: { category: ContextCategory; color?: string; swatch?: string; muted?: boolean; windowTokens?: number }) {
  const [open, setOpen] = useState(false);
  const items = category.items ?? [];
  const marker = <span aria-hidden className={cn("size-2.5 shrink-0 rounded-[3px]", swatch)} style={color ? { backgroundColor: color } : undefined} />;
  const tokens = <span className="ml-auto flex shrink-0 items-baseline gap-2 tabular-nums">
    <span className="text-muted-foreground">{formatTokens(category.tokens)}</span>
    {windowTokens ? <span className="w-11 text-right text-xs font-medium">{`${(category.tokens / windowTokens * 100).toFixed(1)}%`}</span> : null}
  </span>;
  if (!items.length) {
    return <li className={cn("flex min-h-8 items-center gap-2.5 px-1 text-sm", muted && "text-muted-foreground")}>{marker}<span className="truncate">{category.name}</span>{tokens}</li>;
  }
  return <li>
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className={cn("flex min-h-8 w-full items-center gap-2.5 rounded-md px-1 text-left text-sm outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring", muted && "text-muted-foreground")}>
        {marker}
        <span className="truncate">{category.name}</span>
        <ChevronRightIcon aria-hidden className={cn("size-3.5 shrink-0 text-muted-foreground transition-transform motion-reduce:transition-none", open && "rotate-90")} />
        {tokens}
      </CollapsibleTrigger>
      <CollapsiblePanel>
        <ul className="flex flex-col pb-1">
          {items.map((item) => {
            // Memory files arrive as absolute paths: show the file name, the full path on hover.
            const name = /^(?:[a-z]:)?[\\/]/i.test(item.name) ? item.name.split(/[\\/]/).filter(Boolean).pop() ?? item.name : item.name;
            const text = <span className="min-w-0 truncate">{name}{item.detail && <span className="text-muted-foreground"> · {item.detail}</span>}</span>;
            return <li key={item.name} className="flex min-h-7 items-center gap-2.5 ps-5 pe-1 text-[0.8125rem] text-muted-foreground">
              <span aria-hidden className="size-1.5 shrink-0 rounded-full opacity-80" style={{ backgroundColor: color ?? "currentColor" }} />
              {name === item.name ? text : <Tooltip><TooltipTrigger render={<span className="flex min-w-0" />}>{text}</TooltipTrigger><TooltipPopup className="max-w-sm break-all">{item.name}</TooltipPopup></Tooltip>}
              <span className="ml-auto shrink-0 tabular-nums">{formatTokens(item.tokens)}</span>
            </li>;
          })}
        </ul>
      </CollapsiblePanel>
    </Collapsible>
  </li>;
}

/** Harnesses that report only totals (Codex, Oh My Pi) get a plain meter, never an invented split. */
function TotalsOnly({ usage, fraction, percentage }: { usage?: ContextUsage | null; fraction: number | null; percentage: number | null }) {
  const counters = usage ? ([["Input", usage.inputTokens], ["Output", usage.outputTokens], ["Cached", usage.cachedTokens]] as const).filter(([, value]) => value != null) : [];
  return <>
    {fraction !== null && <div role="img" aria-label={`${percentage}% of the context window used`} className="h-2 overflow-hidden rounded-full bg-muted">
      <div className={cn("h-full rounded-full bg-primary", fraction >= 0.85 && "bg-warning")} style={{ width: `${Math.min(100, fraction * 100)}%` }} />
    </div>}
    {counters.length > 0 && <dl className="flex gap-6 text-xs">
      {counters.map(([name, value]) => <div key={name} className="flex flex-col gap-0.5"><dt className="text-muted-foreground">{name}</dt><dd className="tabular-nums">{formatTokens(value ?? 0)}</dd></div>)}
    </dl>}
    <p className="border-t pt-3 text-xs text-muted-foreground">
      {!usage ? "This harness has not reported context usage yet."
        : fraction === null ? "This harness has not reported both the occupancy and the window size."
          : "This harness reports totals only, so there is no per-category breakdown."}
    </p>
  </>;
}
