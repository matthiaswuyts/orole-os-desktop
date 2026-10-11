import { memo, useCallback, useLayoutEffect, useMemo, useRef, useState } from "react";
import { LoaderCircle } from "lucide-react";
import { useVirtualizer } from "@tanstack/react-virtual";
import type { Event as AgentEvent, Session } from "@/mygo";
import { Button } from "@/components/ui/button";
import { ErrorNote } from "@/features/agents/shared";
import { liveActivity, type FeedItem } from "@/lib/taskEvents";
import { AssistantMessage, ErrorRow, FileCard, GuardrailNotice, ImageCard, ImageGenerationCard, NoteRow, PermissionItem, ReasoningBlock, SourcesCard, ToolCard, TurnSeparator, UserMessage } from "./FeedItems";
import { QuestionCard, SubagentCard } from "./TaskInteractions";
import { ConversationSearch, conversationHits, SearchMatches, SearchTrack, stepSearchHit, type SearchHit } from "./ConversationSearch";
import { conversationTimeline, DaySeparator, MessageTime, ToolTimeline, type TimelineEntry } from "./ConversationTimeline";
import { ScrollAnchor, useScrollAnchor } from "./ScrollAnchor";

/** Conversations up to this many rows render every row; longer ones render only the rows near the viewport. */
export const VIRTUALIZE_FROM = 60;
const ESTIMATED_ROW = 96;
const ROW_GAP = 12;
const NO_HITS: readonly SearchHit[] = [];

interface EntryProps { item: FeedItem; sessionId: string; resolved: boolean; session?: Session }

function FeedEntry({ item, sessionId, resolved, session }: EntryProps) {
  switch (item.type) {
    case "user": return <UserMessage item={item} />;
    case "assistant": return <AssistantMessage item={item} />;
    case "reasoning": return <ReasoningBlock item={item} />;
    case "tool": return <ToolCard call={item.call} />;
    case "image": return <ImageCard sessionId={sessionId} item={item} />;
    case "permission": return <PermissionItem sessionId={sessionId} resolved={resolved} item={item} />;
    case "sources": return <SourcesCard sources={item.sources} />;
    case "file": return <FileCard file={item.file} sessionId={sessionId} />;
    case "generation": return <ImageGenerationCard generation={item.generation} image={item.image} sessionId={sessionId} />;
    case "guardrail": return <GuardrailNotice notice={item.notice} />;
    case "question": return session ? <QuestionCard session={session} request={item.request} answers={item.answers} cancelled={item.cancelled} /> : null;
    case "subagent": return <SubagentCard agent={item.agent} />;
    case "note": return <NoteRow item={item} />;
    case "error": return <ErrorRow item={item} />;
    case "turn": return <TurnSeparator item={item} />;
  }
}

interface RowProps {
  entry: TimelineEntry;
  /** A new local day starts at this row. */
  separator: boolean;
  sessionId: string;
  /** Permission rows: the request is no longer pending. */
  resolved: boolean;
  /** Question rows only: the session they answer through. Other rows never depend on the session object. */
  session?: Session;
  active: boolean;
  hits: readonly SearchHit[];
  activeHitId?: string;
}

const sameEntry = (a: TimelineEntry, b: TimelineEntry) => a.type === "item"
  ? b.type === "item" && a.item === b.item
  : b.type === "tools" && a.items.length === b.items.length && a.items.every((item, i) => item === b.items[i]);

/** A row renders again only when its own item, search state or (for questions) session changed, not on every event of the conversation. */
const FeedRow = memo(function FeedRow({ entry, separator, sessionId, resolved, session, active, hits, activeHitId }: RowProps) {
  return <>
    {separator && <DaySeparator at={entry.at} />}
    {entry.type === "tools" ? <ToolTimeline items={entry.items} /> : <div data-scroll-anchor={entry.key} data-search-active={active || undefined} className={active ? "min-w-0 rounded-sm outline-2 outline-offset-4 outline-ring" : "min-w-0"}>
      <FeedEntry item={entry.item} sessionId={sessionId} resolved={resolved} session={session} />
      <SearchMatches hits={hits} activeId={activeHitId} />
      <MessageTime at={entry.at} />
    </div>}
  </>;
}, (a, b) => a.separator === b.separator && a.sessionId === b.sessionId && a.resolved === b.resolved && a.session === b.session && a.active === b.active && a.hits === b.hits && a.activeHitId === b.activeHitId && sameEntry(a.entry, b.entry));

export interface FeedProps {
  session: Session;
  /** The session's feed-relevant events, for counting updates that arrive while the reader is away. */
  events: AgentEvent[];
  /** `events` grouped by `useFeedItems`, shared with the task canvas. */
  items: FeedItem[];
  loading: boolean;
  loadingMore: boolean;
  error: string | null;
  hasMore: boolean;
  /** Resolves true when older events were merged in. */
  onLoadMore: () => Promise<boolean>;
  /** Retries transcript loading, never replays a prompt or a tool. */
  onRetry?: () => void;
}

export function Feed(props: FeedProps) {
  // Session identity resets navigation without leaking the prior conversation's query or pin state.
  return <ConversationFeed key={props.session.id} {...props} />;
}

function ConversationFeed({ session, events, items: grouped, loading, loadingMore, error, hasMore, onLoadMore, onRetry }: FeedProps) {
  const items = useMemo(() => {
    const requests = session.questions ?? [];
    if (!requests.length) return grouped;
    const known = new Set(grouped.flatMap((item) => item.type === "question" ? [item.request.id] : []));
    // A pending input must stay answerable even when its event precedes the loaded history window.
    const missing = requests.filter((request) => !known.has(request.id));
    return missing.length ? [...grouped, ...missing.map((request): FeedItem => ({ type: "question", key: `pending-question:${request.id}`, at: request.at, request }))] : grouped;
  }, [grouped, session.questions]);
  const entries = useMemo(() => conversationTimeline(items), [items]);
  const activity = liveActivity(session);
  const updateIds = useMemo(() => events.filter((event) => event.kind !== "usage").map((event) => event.id), [events]);
  const locate = useRef<(key: string) => number | undefined>(() => undefined);
  const scroll = useScrollAnchor(items, updateIds, (key) => locate.current(key));
  const { reveal, pause } = scroll;
  const [query, setQuery] = useState("");
  const [activeId, setActiveId] = useState<string | null>(null);
  const searchInput = useRef<HTMLInputElement>(null);
  const hits = useMemo(() => conversationHits(items, query), [items, query]);
  const activeIndex = Math.max(0, hits.findIndex((hit) => hit.id === activeId));
  const active = hits[activeIndex];
  const matches = useMemo(() => {
    const result = new Map<string, SearchHit[]>();
    for (const hit of hits) {
      const row = result.get(hit.key);
      if (row) row.push(hit); else result.set(hit.key, [hit]);
    }
    return result;
  }, [hits]);
  const pendingIds = useMemo(() => new Set((session.pending ?? []).map((request) => request.id)), [session.pending]);

  const virtualized = entries.length > VIRTUALIZE_FROM;
  const list = useRef<HTMLDivElement>(null);
  // Rows are positioned relative to the scroll viewport, below whatever the feed shows above the list.
  const [margin, setMargin] = useState(0);
  const virtual = useVirtualizer({
    count: virtualized ? entries.length : 0,
    getScrollElement: () => scroll.viewport.current,
    estimateSize: () => ESTIMATED_ROW,
    gap: ROW_GAP,
    overscan: 8,
    scrollMargin: margin,
    getItemKey: (index) => entries[index]!.key,
  });
  // The scroll anchor keeps the reader's row in place; the virtualizer's own correction on item resizes would fight it.
  virtual.shouldAdjustScrollPositionOnItemSizeChange = () => false;
  const indexOfKey = useRef(new Map<string, number>());
  indexOfKey.current = useMemo(() => new Map(entries.map((entry, index) => [entry.key, index])), [entries]);
  // Where the list's top sits in the scroll viewport's content, read from the DOM; only the blocks above it (error, "Load earlier", loading note) move it.
  const listOffset = () => {
    const viewport = scroll.viewport.current, element = list.current;
    return viewport && element ? Math.round(element.getBoundingClientRect().top - viewport.getBoundingClientRect().top + viewport.scrollTop) : undefined;
  };
  locate.current = (key) => {
    const index = indexOfKey.current.get(key), start = index === undefined ? undefined : virtual.measurementsCache[index]?.start, offset = listOffset();
    // `start` includes the margin this render was given, which can lag the DOM by a render.
    return virtualized && start !== undefined && offset !== undefined ? offset + start - margin : undefined;
  };
  useLayoutEffect(() => {
    const offset = listOffset();
    if (virtualized && offset !== undefined) setMargin(offset);
  }, [virtualized, error, hasMore, loading, loadingMore, scroll.viewport]);

  const hasRow = useCallback((key: string) => Array.from(scroll.content.current?.querySelectorAll<HTMLElement>("[data-scroll-anchor]") ?? []).some((row) => row.dataset.scrollAnchor === key), [scroll.content]);
  // A row outside the rendered window is scrolled into range first, then positioned like any other.
  const pendingReveal = useRef<{ key: string; hitId?: string } | null>(null);
  const revealRow = useCallback((key: string, hitId?: string) => {
    const index = indexOfKey.current.get(key);
    if (index === undefined || !virtualized || hasRow(key)) { reveal(key, hitId); return; }
    pause();
    pendingReveal.current = { key, hitId };
    virtual.scrollToIndex(index, { align: "center" });
  }, [reveal, pause, virtual, virtualized, hasRow]);
  useLayoutEffect(() => {
    const target = pendingReveal.current;
    if (target && hasRow(target.key)) { pendingReveal.current = null; reveal(target.key, target.hitId); }
  });

  // Keep the same hit when paging prepends matches; a new query starts at the first match.
  useLayoutEffect(() => {
    if (active) { setActiveId(active.id); revealRow(active.key, active.id); }
  }, [active?.id, query, revealRow]);

  const step = (delta: number) => {
    const hit = hits[stepSearchHit(activeIndex, delta, hits.length)];
    if (hit) { setActiveId(hit.id); revealRow(hit.key, hit.id); }
  };
  const loadEarlier = () => {
    scroll.pause();
    void onLoadMore();
  };

  const row = (index: number) => {
    const entry = entries[index]!;
    const item = entry.type === "item" ? entry.item : null;
    const hitsHere = matches.get(entry.key) ?? NO_HITS;
    return <FeedRow entry={entry} separator={entry.day !== (index ? entries[index - 1]!.day : null)} sessionId={session.id}
      resolved={item?.type === "permission" ? !pendingIds.has(item.request.id) : false} session={item?.type === "question" ? session : undefined}
      active={active?.key === entry.key} hits={hitsHere} activeHitId={hitsHere.length ? active?.id : undefined} />;
  };

  return <div className="relative flex min-h-0 flex-1 flex-col" onKeyDown={(event) => {
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "f") {
      event.preventDefault(); searchInput.current?.focus(); searchInput.current?.select();
    }
  }}>
    <ConversationSearch query={query} hits={hits} activeIndex={activeIndex} inputRef={searchInput} onQueryChange={(next) => { setQuery(next); setActiveId(null); }} onStep={step} />
    <div className="relative flex min-h-0 flex-1 flex-col">
      <div ref={scroll.viewport} onScroll={scroll.onScroll} onWheel={(event) => { if (event.deltaY < 0) scroll.pause(); }} onTouchStart={scroll.pause} onKeyDown={scroll.onKeyDown} role="log" tabIndex={0} aria-label="Task conversation" aria-live="off" aria-busy={loading} style={{ overflowAnchor: "none" }} className="min-h-0 flex-1 overflow-auto px-5 py-4 pb-14 outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring">
        <div ref={scroll.content} className="mx-auto flex max-w-4xl flex-col gap-3">
          {error && <div className="space-y-2"><ErrorNote error={error} />{onRetry && <Button size="sm" variant="outline" loading={loading || loadingMore} onClick={onRetry}>Retry loading conversation</Button>}</div>}
          {hasMore && <Button size="xs" variant="outline" className="self-center" loading={loadingMore} onClick={loadEarlier}>Load earlier messages</Button>}
          {loading && <p role="status" className="text-muted-foreground">Loading conversation…</p>}
          {!loading && !error && !items.length && <p role="status" className="text-muted-foreground">No output has been recorded yet.</p>}
          {virtualized
            ? <div ref={list} className="relative w-full" style={{ height: virtual.getTotalSize() }}>
              {virtual.getVirtualItems().map((virtualRow) => <div key={virtualRow.key} ref={virtual.measureElement} data-index={virtualRow.index} className="absolute inset-x-0 top-0 flex min-w-0 flex-col gap-3" style={{ transform: `translateY(${virtualRow.start - margin}px)` }}>{row(virtualRow.index)}</div>)}
            </div>
            : entries.map((entry, index) => <div key={entry.key} className="flex min-w-0 flex-col gap-3">{row(index)}</div>)}
          {activity && <p role="status" data-testid="live-activity" className="flex items-center gap-2 text-xs text-muted-foreground"><LoaderCircle aria-hidden className="size-3.5 animate-spin motion-reduce:animate-none" /><span className="min-w-0 truncate">{activity.label}</span></p>}
        </div>
      </div>
      <SearchTrack hits={hits} activeId={active?.id} />
      {scroll.away && <ScrollAnchor unseen={scroll.unseen} onJump={scroll.jump} />}
    </div>
  </div>;
}
