import { memo, useCallback, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { LoaderCircle } from "lucide-react";
import { useVirtualizer } from "@tanstack/react-virtual";
import type { Event as AgentEvent, Session } from "@/mygo";
import { Button } from "@/components/ui/button";
import { RunActivity } from "@/components/assistant-ui/elements/run-activity";
import { ConversationMap } from "@/components/assistant-ui/elements/conversation-map";
import { mapTurns } from "@/lib/conversationMap";
import { ErrorNote } from "@/features/agents/shared";
import { liveActivity, type FeedItem, type LiveActivity, type TurnEntry, type TurnRun } from "@/lib/taskEvents";
import { AssistantMessage, ErrorRow, FileCard, GuardrailNotice, ImageCard, ImageGenerationCard, NoteRow, PermissionItem, SourcesCard, TurnFailure, UserMessage } from "./FeedItems";
import { QuestionCard, SubagentCard } from "./TaskInteractions";
import { ConversationSearch, conversationHits, SearchMatches, SearchTrack, stepSearchHit, type SearchHit } from "./ConversationSearch";
import { conversationDay, DaySeparator } from "./ConversationTimeline";
import { runEntry, runStatusText } from "./RunEntries";
import { ScrollAnchor, useScrollAnchor } from "./ScrollAnchor";
import { useFeedItems, useTurnEntries } from "./useFeedItems";
import { useVisibleTurns } from "./useVisibleTurns";

/** Conversations up to this many entries (a message, or one assistant turn) render every row; longer ones render only the rows near the viewport. */
export const VIRTUALIZE_FROM = 60;
const ESTIMATED_ROW = 96;
const ROW_GAP = 16;

/** The search state rows draw: the excerpts under each message and which one is current. Empty while nothing is searched. */
interface Search { matches: ReadonlyMap<string, readonly SearchHit[]>; activeKey?: string; activeId?: string }
const NO_SEARCH: Search = { matches: new Map(), activeKey: undefined, activeId: undefined };

/** Gives a row the scroll anchor and search outline of its item, with the matching excerpts beneath it. */
const anchored = (key: string, node: ReactNode, search: Search) => <div key={key} data-scroll-anchor={key} data-search-active={search.activeKey === key || undefined} className={search.activeKey === key ? "min-w-0 rounded-sm outline-2 outline-offset-4 outline-ring" : "min-w-0"}>
  {node}
  <SearchMatches hits={search.matches.get(key) ?? []} activeId={search.activeId} />
</div>;

/** An item that lives outside any run's disclosure: a message, a pending request, a file, an image. */
function FeedEntry({ item, session }: { item: FeedItem; session: Session }) {
  switch (item.type) {
    case "user": return <UserMessage item={item} />;
    case "assistant": return <AssistantMessage item={item} />;
    case "image": return <ImageCard sessionId={session.id} item={item} />;
    case "permission": return <PermissionItem session={session} item={item} />;
    case "sources": return <SourcesCard sources={item.sources} />;
    case "file": return <FileCard file={item.file} sessionId={session.id} />;
    case "generation": return <ImageGenerationCard generation={item.generation} image={item.image} sessionId={session.id} />;
    case "guardrail": return <GuardrailNotice notice={item.notice} />;
    case "question": return <QuestionCard session={session} request={item.request} answers={item.answers} cancelled={item.cancelled} />;
    case "subagent": return <SubagentCard agent={item.agent} />;
    case "note": return <NoteRow item={item} />;
    case "error": return <ErrorRow item={item} />;
    case "turn": return <TurnFailure item={item} />;
    case "reasoning":
    case "tool": return null; // always folded into a run
  }
}

const needsSessionItem = (item: FeedItem) => item.type === "permission" || item.type === "question";
/** Only permission and question rows read the session (who is asking, what is still pending); every other row depends on its items alone. */
const needsSession = (entry: TurnEntry) => entry.type === "item" ? needsSessionItem(entry.item) : entry.steps.some(needsSessionItem) || entry.attention.some(needsSessionItem) || entry.answer.some(needsSessionItem);

interface RowProps {
  entry: TurnEntry;
  /** A new local day starts at this row. */
  separator: boolean;
  session: Session;
  search: Search;
  /** Run rows: the disclosure is open. */
  open: boolean;
  /** Run rows: what the agent is doing, for the running one only. */
  activity: LiveActivity | null;
  onOpenChange: (key: string, open: boolean) => void;
}

function RunView({ run, session, search, open, activity, onOpenChange }: Omit<RowProps, "entry" | "separator"> & { run: TurnRun }) {
  const stepEntries = run.steps.flatMap((item) => runEntry(item, session, (key) => search.matches.has(key) ? anchored(key, null, search) : null) ?? []);
  const { label, duration } = runStatusText(run.status, run.durationMs, activity);
  const itemView = (item: FeedItem) => anchored(item.key, <FeedEntry item={item} session={session} />, search);
  return <div data-scroll-anchor={run.key} className="min-w-0"><RunActivity
    data-testid={run.status === "running" ? "live-activity" : undefined}
    status={run.status}
    statusLabel={label}
    durationLabel={duration}
    open={open}
    onOpenChange={(next) => onOpenChange(run.key, next)}
    entries={stepEntries}
    attention={run.attention.length ? <div className="flex flex-col gap-3">{run.attention.map(itemView)}</div> : undefined}
  >
    {run.answer.length > 0 && <div className="flex flex-col gap-3">{run.answer.map(itemView)}</div>}
  </RunActivity></div>;
}

/**
 * `useTurnEntries` keeps the identity of an entry no update touched, so a row renders again only when its own entry, disclosure, search state or (for permission and question rows) session changed,
 * not on every event of the conversation.
 */
const FeedRow = memo(function FeedRow({ entry, separator, session, search, open, activity, onOpenChange }: RowProps) {
  return <>
    {separator && <DaySeparator at={entry.at} />}
    {entry.type === "run"
      ? <RunView run={entry} session={session} search={search} open={open} activity={activity} onOpenChange={onOpenChange} />
      : anchored(entry.item.key, <FeedEntry item={entry.item} session={session} />, search)}
  </>;
}, (a, b) => a.separator === b.separator && a.search === b.search && a.open === b.open && a.activity?.label === b.activity?.label && a.onOpenChange === b.onOpenChange
  && a.session.id === b.session.id && (a.session === b.session || !needsSession(b.entry)) && a.entry === b.entry);

export interface FeedProps {
  session: Session;
  events: AgentEvent[];
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

function ConversationFeed({ session, events, loading, loadingMore, error, hasMore, onLoadMore, onRetry }: FeedProps) {
  const grouped = useFeedItems(events, session.live);
  const items = useMemo(() => {
    const requests = session.questions ?? [];
    if (!requests.length) return grouped;
    const known = new Set(grouped.flatMap((item) => item.type === "question" ? [item.request.id] : []));
    // A pending input must stay answerable even when its event precedes the loaded history window.
    const missing = requests.filter((request) => !known.has(request.id));
    return missing.length ? [...grouped, ...missing.map((request): FeedItem => ({ type: "question", key: `pending-question:${request.id}`, at: request.at, request }))] : grouped;
  }, [grouped, session.questions]);
  const activity = liveActivity(session);
  const running = activity !== null;
  const entries = useTurnEntries(items, running, session.live, session.pending, session.questions);
  // Run-disclosure state per run key: collapsed by default, and kept through new activity, completion and scrolling a run out of the window.
  const [openRuns, setOpenRuns] = useState<Record<string, boolean>>({});
  const days = useMemo(() => entries.map((entry) => conversationDay(entry.at)), [entries]);
  /** Where each item sits: the index of its entry, and for a message folded into a run, that run's key. */
  const { indexOfKey, runOfMessage } = useMemo(() => {
    const index = new Map<string, number>();
    const run = new Map<string, string>();
    entries.forEach((entry, i) => {
      index.set(entry.key, i);
      if (entry.type === "item") { index.set(entry.item.key, i); return; }
      for (const list of [entry.steps, entry.attention, entry.answer]) for (const item of list) index.set(item.key, i);
      for (const step of entry.steps) if (step.type === "assistant") run.set(step.key, entry.key);
    });
    return { indexOfKey: index, runOfMessage: run };
  }, [entries]);
  const map = useMemo(() => mapTurns(entries), [entries]);
  const updateIds = useMemo(() => events.filter((event) => event.kind !== "usage").map((event) => event.id), [events]);
  const locate = useRef<(key: string) => number | undefined>(() => undefined);
  const scroll = useScrollAnchor(items, updateIds, (key) => locate.current(key));
  const { reveal, pause } = scroll;
  const mapState = useVisibleTurns({ viewport: scroll.viewport, content: scroll.content, turns: map.turns, indexOfKey, turnOfEntry: map.turnOfEntry });
  const [query, setQuery] = useState("");
  const [searchOpen, setSearchOpen] = useState(false);
  const [activeId, setActiveId] = useState<string | null>(null);
  const [revealTick, setRevealTick] = useState(0);
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
  const search = useMemo<Search>(() => hits.length ? { matches, activeKey: active?.key, activeId: active?.id } : NO_SEARCH, [hits.length, matches, active?.key, active?.id]);

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
  const entryIndex = useRef(indexOfKey);
  entryIndex.current = indexOfKey;
  // Where the list's top sits in the scroll viewport's content, read from the DOM; only the blocks above it (error, "Load earlier", loading note) move it.
  const listOffset = () => {
    const viewport = scroll.viewport.current, element = list.current;
    return viewport && element ? Math.round(element.getBoundingClientRect().top - viewport.getBoundingClientRect().top + viewport.scrollTop) : undefined;
  };
  locate.current = (key) => {
    const index = entryIndex.current.get(key), start = index === undefined ? undefined : virtual.measurementsCache[index]?.start, offset = listOffset();
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
    const index = entryIndex.current.get(key);
    if (index === undefined || !virtualized || hasRow(key)) { reveal(key, hitId); return; }
    pause();
    pendingReveal.current = { key, hitId };
    virtual.scrollToIndex(index, { align: "center" });
  }, [reveal, pause, virtual, virtualized, hasRow]);
  useLayoutEffect(() => {
    const target = pendingReveal.current;
    if (target && hasRow(target.key)) { pendingReveal.current = null; reveal(target.key, target.hitId); }
  });

  // A hit inside a collapsed run opens that run first; the reveal below then scrolls to the mounted match.
  const hitRun = active ? runOfMessage.get(active.key) : undefined;
  const hitVisible = !hitRun || openRuns[hitRun] === true;
  useLayoutEffect(() => {
    if (hitRun && !openRuns[hitRun]) setOpenRuns((open) => ({ ...open, [hitRun]: true }));
  }, [hitRun, active?.id, revealTick]);
  useLayoutEffect(() => {
    if (!active || !hitVisible) return;
    setActiveId(active.id);
    revealRow(active.key, active.id);
    // An expanding panel may mount its content a frame later.
    const frame = hitRun ? requestAnimationFrame(() => revealRow(active.key, active.id)) : 0;
    return () => { if (frame) cancelAnimationFrame(frame); };
  }, [active?.id, query, revealRow, hitVisible, revealTick]);
  useLayoutEffect(() => {
    if (searchOpen) { searchInput.current?.focus(); searchInput.current?.select(); }
  }, [searchOpen]);
  const step = (delta: number) => {
    const hit = hits[stepSearchHit(activeIndex, delta, hits.length)];
    if (hit) { setActiveId(hit.id); setRevealTick((tick) => tick + 1); }
  };
  const loadEarlier = () => {
    scroll.pause();
    void onLoadMore();
  };
  const toggleRun = useCallback((key: string, next: boolean) => { pause(); setOpenRuns((state) => ({ ...state, [key]: next })); }, [pause]);

  const lastRun = entries.at(-1);
  const liveRun = lastRun?.type === "run" && lastRun.status === "running";
  const row = (index: number) => {
    const entry = entries[index]!;
    return <FeedRow entry={entry} separator={days[index] !== (index ? days[index - 1] : null)} session={session} search={search}
      open={entry.type === "run" && openRuns[entry.key] === true} activity={entry.type === "run" && entry.status === "running" ? activity : null} onOpenChange={toggleRun} />;
  };

  return <div className="relative flex min-h-0 flex-1 flex-col" onKeyDown={(event) => {
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "f") {
      event.preventDefault(); setSearchOpen(true);
    }
  }}>
    {searchOpen && <ConversationSearch query={query} hits={hits} activeIndex={activeIndex} inputRef={searchInput} onQueryChange={(next) => { setQuery(next); setActiveId(null); }} onStep={step} onClose={() => { setQuery(""); setActiveId(null); setSearchOpen(false); }} />}
    <div className="@container/feed relative flex min-h-0 flex-1 flex-col">
      <div ref={scroll.viewport} onScroll={scroll.onScroll} onWheel={(event) => { if (event.deltaY < 0) scroll.pause(); }} onTouchStart={scroll.pause} onKeyDown={scroll.onKeyDown} role="log" tabIndex={0} aria-label="Task conversation" aria-live="off" aria-busy={loading} style={{ overflowAnchor: "none" }} className="min-h-0 flex-1 overflow-auto px-5 py-4 pb-14 outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring">
        <div ref={scroll.content} className="mx-auto flex max-w-4xl flex-col gap-4">
          {error && <div className="space-y-2"><ErrorNote error={error} />{onRetry && <Button size="sm" variant="outline" loading={loading || loadingMore} onClick={onRetry}>Retry loading conversation</Button>}</div>}
          {hasMore && <Button size="xs" variant="outline" className="self-center" loading={loadingMore} onClick={loadEarlier}>Load earlier messages</Button>}
          {loading && <p role="status" className="text-muted-foreground">Loading conversation…</p>}
          {!loading && !error && !items.length && <p role="status" className="text-muted-foreground">No output has been recorded yet.</p>}
          {virtualized
            ? <div ref={list} className="relative w-full" style={{ height: virtual.getTotalSize() }}>
              {virtual.getVirtualItems().map((virtualRow) => <div key={virtualRow.key} ref={virtual.measureElement} data-index={virtualRow.index} className="absolute inset-x-0 top-0 flex min-w-0 flex-col gap-4" style={{ transform: `translateY(${virtualRow.start - margin}px)` }}>{row(virtualRow.index)}</div>)}
            </div>
            : entries.map((entry, index) => <div key={entry.key} className="flex min-w-0 flex-col gap-4">{row(index)}</div>)}
          {activity && !liveRun && <p role="status" data-testid="live-activity" className="flex items-center gap-2 text-xs text-muted-foreground"><LoaderCircle aria-hidden className="size-3.5 animate-spin motion-reduce:animate-none" /><span className="cv-shimmer min-w-0 truncate font-medium">{activity.label}</span></p>}
        </div>
      </div>
      <SearchTrack hits={hits} activeId={active?.id} />
      {map.turns.length > 1 && <ConversationMap side="left" entries={map.turns} activeId={mapState.activeId} visibleIds={mapState.visibleIds} onSelect={(id) => revealRow(id)}
        className="absolute top-1/2 right-1.5 z-10 hidden max-h-[70%] -translate-y-1/2 @5xl/feed:flex" />}
      {scroll.away && <ScrollAnchor unseen={scroll.unseen} onJump={scroll.jump} />}
    </div>
  </div>;
}
