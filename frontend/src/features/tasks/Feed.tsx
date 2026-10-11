import { Fragment, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { LoaderCircle } from "lucide-react";
import type { Event as AgentEvent, Session } from "@/mygo";
import { Button } from "@/components/ui/button";
import { RunActivity } from "@/components/assistant-ui/elements/run-activity";
import { ErrorNote } from "@/features/agents/shared";
import { groupEvents, groupRuns, liveActivity, type FeedItem, type TurnRun } from "@/lib/taskEvents";
import { AssistantMessage, ErrorRow, FileCard, GuardrailNotice, ImageCard, ImageGenerationCard, NoteRow, PermissionItem, SourcesCard, stamp, TurnFailure, UserMessage } from "./FeedItems";
import { QuestionCard, SubagentCard } from "./TaskInteractions";
import { ConversationSearch, conversationHits, SearchMatches, SearchTrack, stepSearchHit } from "./ConversationSearch";
import { conversationDay, DaySeparator } from "./ConversationTimeline";
import { runEntry, runStatusText } from "./RunEntries";
import { ScrollAnchor, useScrollAnchor } from "./ScrollAnchor";

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
  const items = useMemo(() => {
    const grouped = groupEvents(events, { live: session.live });
    const questionIds = new Set(grouped.flatMap((item) => item.type === "question" ? [item.request.id] : []));
    // A pending input must stay answerable even when its event precedes the loaded history window.
    for (const request of session.questions ?? []) {
      if (!questionIds.has(request.id)) grouped.push({ type: "question", key: `pending-question:${request.id}`, at: request.at, request });
    }
    return grouped;
  }, [events, session.live, session.questions]);
  const activity = liveActivity(session);
  const running = activity !== null;
  const entries = useMemo(() => groupRuns(items, {
    running, live: session.live,
    pendingPermissions: new Set(session.pending?.map((request) => request.id)),
    pendingQuestions: new Set(session.questions?.map((request) => request.id)),
  }), [items, running, session.live, session.pending, session.questions]);
  // Run-disclosure state per run key: collapsed by default, and kept through new activity and completion.
  const [openRuns, setOpenRuns] = useState<Record<string, boolean>>({});
  const runOfMessage = useMemo(() => {
    const result = new Map<string, string>();
    for (const entry of entries) if (entry.type === "run") for (const step of entry.steps) if (step.type === "assistant") result.set(step.key, entry.key);
    return result;
  }, [entries]);
  const updateIds = useMemo(() => events.filter((event) => event.kind !== "usage").map((event) => event.id), [events]);
  const scroll = useScrollAnchor(items, updateIds);
  const [query, setQuery] = useState("");
  const [searchOpen, setSearchOpen] = useState(false);
  const [activeId, setActiveId] = useState<string | null>(null);
  const [revealTick, setRevealTick] = useState(0);
  const searchInput = useRef<HTMLInputElement>(null);
  const hits = useMemo(() => conversationHits(items, query), [items, query]);
  const activeIndex = Math.max(0, hits.findIndex((hit) => hit.id === activeId));
  const active = hits[activeIndex];
  const matches = useMemo(() => {
    const result = new Map<string, typeof hits>();
    for (const hit of hits) {
      const row = result.get(hit.key);
      if (row) row.push(hit); else result.set(hit.key, [hit]);
    }
    return result;
  }, [hits]);
  // A hit inside a collapsed run opens that run first; the reveal below then scrolls to the mounted match.
  const hitRun = active ? runOfMessage.get(active.key) : undefined;
  const hitVisible = !hitRun || openRuns[hitRun] === true;

  useLayoutEffect(() => {
    if (hitRun && !openRuns[hitRun]) setOpenRuns((open) => ({ ...open, [hitRun]: true }));
  }, [hitRun, active?.id, revealTick]);
  useLayoutEffect(() => {
    if (!active || !hitVisible) return;
    setActiveId(active.id);
    scroll.reveal(active.key, active.id);
    // An expanding panel may mount its content a frame later.
    const frame = hitRun ? requestAnimationFrame(() => scroll.reveal(active.key, active.id)) : 0;
    return () => { if (frame) cancelAnimationFrame(frame); };
  }, [active?.id, query, scroll.reveal, hitVisible, revealTick]);
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

  /** Gives a row the scroll anchor and search outline of its item, with the matching excerpts beneath it. */
  const anchored = (key: string, node: ReactNode) => <div key={key} data-scroll-anchor={key} data-search-active={active?.key === key || undefined} className={active?.key === key ? "min-w-0 rounded-sm outline-2 outline-offset-4 outline-ring" : "min-w-0"}>
    {node}
    <SearchMatches hits={matches.get(key) ?? []} activeId={active?.id} />
  </div>;
  const itemView = (item: FeedItem) => anchored(item.key, <FeedEntry item={item} session={session} />);

  const renderRun = (run: TurnRun) => {
    const stepEntries = run.steps.flatMap((item) => runEntry(item, session, (key) => matches.has(key) ? anchored(key, null) : null) ?? []);
    const { label, duration } = runStatusText(run.status, run.durationMs, activity);
    const open = openRuns[run.key] === true;
    return <RunActivity
      key={run.key}
      data-testid={run.status === "running" ? "live-activity" : undefined}
      title={stamp(run.at)}
      status={run.status}
      statusLabel={label}
      durationLabel={duration}
      open={open}
      onOpenChange={(next) => { scroll.pause(); setOpenRuns((state) => ({ ...state, [run.key]: next })); }}
      entries={stepEntries}
      attention={run.attention.length ? <div className="flex flex-col gap-3">{run.attention.map(itemView)}</div> : undefined}
    >
      {run.answer.length > 0 && <div className="flex flex-col gap-3">{run.answer.map(itemView)}</div>}
    </RunActivity>;
  };
  const liveRun = entries.at(-1)?.type === "run" && (entries.at(-1) as TurnRun).status === "running";

  return <div className="relative flex min-h-0 flex-1 flex-col" onKeyDown={(event) => {
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "f") {
      event.preventDefault(); setSearchOpen(true);
    }
  }}>
    {searchOpen && <ConversationSearch query={query} hits={hits} activeIndex={activeIndex} inputRef={searchInput} onQueryChange={(next) => { setQuery(next); setActiveId(null); }} onStep={step} onClose={() => { setQuery(""); setActiveId(null); setSearchOpen(false); }} />}
    <div className="relative flex min-h-0 flex-1 flex-col">
      <div ref={scroll.viewport} onScroll={scroll.onScroll} onWheel={(event) => { if (event.deltaY < 0) scroll.pause(); }} onTouchStart={scroll.pause} onKeyDown={scroll.onKeyDown} role="log" tabIndex={0} aria-label="Task conversation" aria-live="off" aria-busy={loading} style={{ overflowAnchor: "none" }} className="min-h-0 flex-1 overflow-auto px-5 py-4 pb-14 outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring">
        <div ref={scroll.content} className="mx-auto flex max-w-4xl flex-col gap-4">
          {error && <div className="space-y-2"><ErrorNote error={error} />{onRetry && <Button size="sm" variant="outline" loading={loading || loadingMore} onClick={onRetry}>Retry loading conversation</Button>}</div>}
          {hasMore && <Button size="xs" variant="outline" className="self-center" loading={loadingMore} onClick={loadEarlier}>Load earlier messages</Button>}
          {loading && <p role="status" className="text-muted-foreground">Loading conversation…</p>}
          {!loading && !error && !items.length && <p role="status" className="text-muted-foreground">No output has been recorded yet.</p>}
          {entries.map((entry, index) => <Fragment key={entry.key}>
            {conversationDay(entry.at) !== (index ? conversationDay(entries[index - 1]!.at) : null) && <DaySeparator at={entry.at} />}
            {entry.type === "run" ? renderRun(entry) : itemView(entry.item)}
          </Fragment>)}
          {activity && !liveRun && <p role="status" data-testid="live-activity" className="flex items-center gap-2 text-xs text-muted-foreground"><LoaderCircle aria-hidden className="size-3.5 animate-spin motion-reduce:animate-none" /><span className="cv-shimmer min-w-0 truncate font-medium">{activity.label}</span></p>}
        </div>
      </div>
      <SearchTrack hits={hits} activeId={active?.id} />
      {scroll.away && <ScrollAnchor unseen={scroll.unseen} onJump={scroll.jump} />}
    </div>
  </div>;
}
