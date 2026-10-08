import type { ContextUsage } from "@/mygo";
import { CircleHelpIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Popover, PopoverPopup, PopoverTitle, PopoverTrigger } from "@/components/ui/popover";
import { Progress } from "@/components/ui/progress";
import { Separator } from "@/components/ui/separator";
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
  const number = (value?: number | null) => value == null ? "Not reported" : new Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: 1 }).format(value);
  const label = percentage === null ? "Context usage: not fully reported" : `Context usage: ${percentage}%`;
  return <Popover>
    <PopoverTrigger render={<Button type="button" variant="ghost" size="sm" className="h-7 gap-1.5 rounded-full px-2 text-muted-foreground tabular-nums" aria-label={label} />}>
      {fraction === null ? <CircleHelpIcon aria-hidden data-icon="inline-start" /> : <svg aria-hidden viewBox="0 0 24 24" className={cn("size-4", fraction >= 0.85 && "text-warning")}>
        <circle cx="12" cy="12" r="9" fill="none" stroke="currentColor" strokeWidth="2" opacity="0.2" />
        <circle cx="12" cy="12" r="9" fill="none" stroke="currentColor" strokeWidth="2" pathLength="1" strokeDasharray="1" strokeDashoffset={1 - Math.min(1, fraction)} transform="rotate(-90 12 12)" />
      </svg>}
      {percentage === null ? "Context" : `${percentage}%`}
    </PopoverTrigger>
    <PopoverPopup side="top" align="end" sideOffset={8} className="w-80 max-w-[calc(100vw-2rem)]">
      <div className="flex flex-col gap-4">
        <div className="flex items-center justify-between gap-4">
          <PopoverTitle>Context window</PopoverTitle>
          <span className={cn("text-sm font-medium tabular-nums", fraction !== null && fraction >= 0.85 && "text-warning")}>{percentage === null ? "Unknown" : `${percentage}% used`}</span>
        </div>
        <div className="flex flex-col gap-2">
          {fraction !== null && <Progress aria-label="Context window used" value={Math.min(100, percentage ?? 0)} />}
          <dl className="flex justify-between gap-4 text-xs">
            <div className="flex flex-col gap-1"><dt className="text-muted-foreground">Used tokens</dt><dd className="tabular-nums" title={used?.toLocaleString()}>{number(used)}</dd></div>
            <div className="flex flex-col gap-1 text-right"><dt className="text-muted-foreground">Window size</dt><dd className="tabular-nums" title={limit?.toLocaleString()}>{number(limit)}</dd></div>
          </dl>
        </div>
        {usage && <>
          <Separator />
          <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-xs">
            <dt className="text-muted-foreground">Input tokens</dt><dd className="text-right tabular-nums">{number(usage.inputTokens)}</dd>
            <dt className="text-muted-foreground">Output tokens</dt><dd className="text-right tabular-nums">{number(usage.outputTokens)}</dd>
            <dt className="text-muted-foreground">Cached tokens</dt><dd className="text-right tabular-nums">{number(usage.cachedTokens)}</dd>
          </dl>
        </>}
        <p className="text-xs text-muted-foreground">{usage ? "Provider usage counters, not a context breakdown." : "This provider has not reported context usage."}</p>
      </div>
    </PopoverPopup>
  </Popover>;
}
