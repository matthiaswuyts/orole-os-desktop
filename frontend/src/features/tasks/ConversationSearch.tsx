import { useId } from "react";
import type { Ref } from "react";
import { ArrowDown, ArrowUp, Search, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import type { FeedItem } from "@/lib/taskEvents";

export interface SearchHit { id: string; key: string; before: string; match: string; after: string; position: number }

/** Literal, non-overlapping matches in loaded message text; never a regular expression. */
export function conversationHits(items: readonly FeedItem[], query: string): SearchHit[] {
  if (!query.trim()) return [];
  const hits: SearchHit[] = [];
  // Case-insensitive regex preserves original UTF-16 offsets (lowercasing can change string length).
  const pattern = new RegExp(query.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"), "giu");
  items.forEach((item, index) => {
    if (item.type !== "user" && item.type !== "assistant") return;
    for (const match of item.text.matchAll(pattern)) {
      const start = match.index, end = start + match[0].length;
      hits.push({ id: `${item.key}:${start}`, key: item.key, before: item.text.slice(Math.max(0, start - 36), start), match: match[0], after: item.text.slice(end, end + 36), position: items.length > 1 ? index / (items.length - 1) * 100 : 0 });
    }
  });
  return hits;
}

export function stepSearchHit(current: number, delta: number, length: number): number {
  return length ? ((current + delta) % length + length) % length : -1;
}

export function ConversationSearch({ query, hits, activeIndex, onQueryChange, onStep, inputRef }: {
  query: string; hits: readonly SearchHit[]; activeIndex: number;
  onQueryChange: (query: string) => void; onStep: (delta: number) => void; inputRef?: Ref<HTMLInputElement>;
}) {
  const scopeId = useId();
  const active = hits[activeIndex];
  return <div data-slot="conversation-search" className="shrink-0 border-b px-5 py-2">
    <div className="mx-auto max-w-4xl space-y-1.5">
      <div className="flex items-center gap-1.5">
        <Search aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
        <Input nativeInput ref={inputRef} size="sm" type="search" aria-label="Find in conversation" aria-describedby={scopeId} placeholder="Find in conversation" value={query} onChange={(event) => onQueryChange(event.target.value)} onKeyDown={(event) => {
          if (event.key === "Enter") { event.preventDefault(); onStep(event.shiftKey ? -1 : 1); }
          if (event.key === "Escape") { event.preventDefault(); onQueryChange(""); }
        }} />
        <span role="status" className="min-w-12 shrink-0 text-center text-xs tabular-nums text-muted-foreground">{query.trim() ? `${active ? activeIndex + 1 : 0}/${hits.length}` : ""}</span>
        <Button size="icon-sm" variant="ghost" aria-label="Previous match" disabled={!hits.length} onClick={() => onStep(-1)}><ArrowUp aria-hidden /></Button>
        <Button size="icon-sm" variant="ghost" aria-label="Next match" disabled={!hits.length} onClick={() => onStep(1)}><ArrowDown aria-hidden /></Button>
        {query && <Button size="icon-sm" variant="ghost" aria-label="Clear conversation search" onClick={() => onQueryChange("")}><X aria-hidden /></Button>}
      </div>
      <p id={scopeId} className="text-xs text-muted-foreground">Searches loaded user and assistant messages only. Load earlier messages to search more history.</p>
      {query.trim() && !hits.length && <p className="text-xs text-muted-foreground">No matches in loaded messages.</p>}
      {active && <p className="truncate text-xs" aria-label="Active search match">{active.before}<mark className="rounded-sm bg-primary/20 text-foreground">{active.match}</mark>{active.after}</p>}
    </div>
  </div>;
}

/** Highlighted excerpts also work for Markdown matches split across inline formatting. */
export function SearchMatches({ hits, activeId }: { hits: readonly SearchHit[]; activeId?: string }) {
  if (!hits.length) return null;
  return <div aria-label="Search matches in this message" className="mt-1 space-y-1 text-xs text-muted-foreground">
    {hits.map((hit) => <p key={hit.id} data-search-hit={hit.id} aria-current={hit.id === activeId ? "true" : undefined} className="break-words">
      {hit.before}<mark className={hit.id === activeId ? "rounded-sm bg-primary text-primary-foreground" : "rounded-sm bg-primary/20 text-foreground"}>{hit.match}</mark>{hit.after}
    </p>)}
  </div>;
}

export function SearchTrack({ hits, activeId }: { hits: readonly SearchHit[]; activeId?: string }) {
  if (!hits.length) return null;
  // Decorative marks avoid hundreds of overlapping/tiny keyboard targets; navigation stays in the find bar.
  return <div aria-hidden className="pointer-events-none absolute inset-y-3 right-1 w-1 rounded-full bg-muted">
    {hits.map((hit) => <span key={hit.id} className={`absolute h-1 w-1 rounded-full ${hit.id === activeId ? "bg-primary" : "bg-muted-foreground/60"}`} style={{ top: `${hit.position}%` }} />)}
  </div>;
}
