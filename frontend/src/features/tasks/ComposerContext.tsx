import type { ContextUsage } from "@/mygo";
import { CircleHelpIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Popover, PopoverPopup, PopoverTitle, PopoverTrigger } from "@/components/ui/popover";
import { cn } from "@/lib/utils";

/** Unknown counts stay unknown; billing totals are never used as context occupancy. */
export function contextUsage(usage?: ContextUsage | null) {
  const count = (value?: number | null) => typeof value === "number" && Number.isFinite(value) && value >= 0 ? value : null;
  const used = count(usage?.contextTokens);
  const limit = count(usage?.contextWindow);
  const fraction = used !== null && limit !== null && limit > 0 ? used / limit : null;
  return { used, limit, fraction };
}

export function ComposerContext({ usage }: { usage?: ContextUsage | null }) {
  const { used, limit, fraction } = contextUsage(usage);
  const percentage = fraction === null ? null : Math.round(fraction * 100);
  const number = (value?: number | null) => value == null ? "Not reported" : value.toLocaleString();
  const label = percentage === null ? "Context usage: not fully reported" : `Context usage: ${percentage}%`;
  return <Popover>
    <PopoverTrigger render={<Button type="button" variant="ghost" size="sm" aria-label={label} />}>
      {fraction === null ? <CircleHelpIcon aria-hidden data-icon="inline-start" /> : <svg aria-hidden viewBox="0 0 24 24" className={cn("size-4", fraction >= 0.85 && "text-warning")}>
        <circle cx="12" cy="12" r="9" fill="none" stroke="currentColor" strokeWidth="2" opacity="0.2" />
        <circle cx="12" cy="12" r="9" fill="none" stroke="currentColor" strokeWidth="2" pathLength="1" strokeDasharray="1" strokeDashoffset={1 - Math.min(1, fraction)} transform="rotate(-90 12 12)" />
      </svg>}
      {percentage === null ? "Context" : `${percentage}%`}
    </PopoverTrigger>
    <PopoverPopup side="top" align="start" className="w-72">
      <div className="flex flex-col gap-3">
        <PopoverTitle>Context usage</PopoverTitle>
        <dl className="grid grid-cols-2 gap-x-3 gap-y-2 text-xs">
          <dt>Context tokens</dt><dd className="text-right tabular-nums">{number(used)}</dd>
          <dt>Context limit</dt><dd className="text-right tabular-nums">{number(limit)}</dd>
          <dt>Input tokens</dt><dd className="text-right tabular-nums">{number(usage?.inputTokens)}</dd>
          <dt>Output tokens</dt><dd className="text-right tabular-nums">{number(usage?.outputTokens)}</dd>
          <dt>Cached tokens</dt><dd className="text-right tabular-nums">{number(usage?.cachedTokens)}</dd>
        </dl>
        <p className="text-xs text-muted-foreground">{usage ? `Reported by ${usage.source || "the provider"}. Input, output and cache counts are usage counters, not a context breakdown.` : "This provider has not reported token usage or its context limit."}</p>
        {usage?.at && <p className="text-xs text-muted-foreground">Observed <time dateTime={usage.at}>{new Date(usage.at).toLocaleString()}</time></p>}
      </div>
    </PopoverPopup>
  </Popover>;
}
