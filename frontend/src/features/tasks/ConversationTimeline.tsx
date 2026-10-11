import { memo, useState } from "react";
import type { FeedItem } from "@/lib/taskEvents";
import { ToolCard } from "./FeedItems";

/** Local calendar identity includes the year and does not conflate UTC and local midnight. */
export function conversationDay(at: string): string | null {
  const date = new Date(at);
  return Number.isNaN(date.getTime()) ? null : `${date.getFullYear()}-${date.getMonth() + 1}-${date.getDate()}`;
}

export function DaySeparator({ at }: { at: string }) {
  const date = new Date(at);
  if (Number.isNaN(date.getTime())) return null;
  const label = date.toLocaleDateString(undefined, { weekday: "short", year: "numeric", month: "short", day: "numeric" });
  return <div role="separator" aria-label={label} data-slot="day-separator" className="my-2 flex items-center gap-3 text-xs text-muted-foreground">
    <span aria-hidden className="h-px flex-1 bg-border" /><time dateTime={at}>{label}</time><span aria-hidden className="h-px flex-1 bg-border" />
  </div>;
}

export function MessageTime({ at }: { at: string }) {
  const date = new Date(at);
  if (Number.isNaN(date.getTime())) return null;
  return <time dateTime={at} title={date.toLocaleString()} className="block text-right text-[11px] tabular-nums text-muted-foreground">{date.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" })}</time>;
}

export type ToolItem = Extract<FeedItem, { type: "tool" }>;
export type TimelineEntry = { type: "item"; key: string; at: string; day: string | null; item: FeedItem } | { type: "tools"; key: string; at: string; day: string | null; items: ToolItem[] };

/** Only adjacent calls share a trace; never move a tool past a message or a local-day boundary. `day` is each entry's local calendar day, computed once. */
export function conversationTimeline(items: readonly FeedItem[]): TimelineEntry[] {
  const entries: TimelineEntry[] = [];
  for (const item of items) {
    const prior = entries[entries.length - 1];
    const day = conversationDay(item.at);
    if (item.type === "tool") {
      if (prior?.type === "tools" && prior.day === day) prior.items.push(item);
      else entries.push({ type: "tools", key: item.key, at: item.at, day, items: [item] });
    } else entries.push({ type: "item", key: item.key, at: item.at, day, item });
  }
  return entries;
}

const ToolRow = memo(function ToolRow({ item }: { item: ToolItem }) {
  return <li data-scroll-anchor={item.key} className="min-w-0">
    <ToolCard call={item.call} /><MessageTime at={item.at} />
  </li>;
});

/** Renders again only when a call of the group changed, not on every event of the conversation. */
export const ToolTimeline = memo(function ToolTimeline({ items }: { items: readonly ToolItem[] }) {
  const [open, setOpen] = useState(true);
  const running = items.filter((item) => item.call.status === "running").length;
  const failed = items.filter((item) => item.call.status === "error").length;
  return <details data-slot="tool-timeline" open={open} onToggle={(event) => setOpen(event.currentTarget.open)}>
    <summary className="cursor-pointer rounded-sm py-1 text-xs text-muted-foreground focus-visible:outline-2 focus-visible:outline-ring">
      {items.length} tool {items.length === 1 ? "call" : "calls"}{running ? ` · ${running} running` : ""}{failed ? ` · ${failed} failed` : ""}
    </summary>
    <ol aria-label="Tool calls in recorded order" className="ml-1.5 space-y-2 border-l pl-3">
      {items.map((item) => <ToolRow key={item.key} item={item} />)}
    </ol>
  </details>;
}, (a, b) => a.items.length === b.items.length && a.items.every((item, i) => item === b.items[i]));
