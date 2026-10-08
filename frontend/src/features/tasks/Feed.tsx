import { Fragment, useLayoutEffect, useMemo, useRef, useState } from "react";
import { LoaderCircle } from "lucide-react";
import type { Event as AgentEvent, Session } from "@/mygo";
import { Button } from "@/components/ui/button";
import { ErrorNote } from "@/features/agents/shared";
import { groupEvents, liveActivity, type FeedItem } from "@/lib/taskEvents";
import { AssistantMessage, ErrorRow, FileCard, GuardrailNotice, ImageCard, ImageGenerationCard, NoteRow, PermissionItem, ReasoningBlock, SourcesCard, ToolCard, TurnSeparator, UserMessage } from "./FeedItems";
import { QuestionCard, SubagentCard } from "./TaskInteractions";
import { ConversationSearch, conversationHits, SearchMatches, SearchTrack, stepSearchHit } from "./ConversationSearch";
import { conversationDay, conversationTimeline, DaySeparator, MessageTime, ToolTimeline } from "./ConversationTimeline";
import { ScrollAnchor, useScrollAnchor } from "./ScrollAnchor";

function FeedEntry({ item, session }: { item: FeedItem; session: Session }) {
  switch (item.type) {
    case "user": return <UserMessage item={item} />;
    case "assistant": return <AssistantMessage item={item} />;
    case "reasoning": return <ReasoningBlock item={item} />;
    case "tool": return <ToolCard call={item.call} />;
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
    case "turn": return <TurnSeparator item={item} />;
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
  const entries = useMemo(() => conversationTimeline(items), [items]);
  const activity = liveActivity(session);
  const updateIds = useMemo(() => events.filter((event) => event.kind !== "usage").map((event) => event.id), [events]);
  const scroll = useScrollAnchor(items, updateIds);
  const [query, setQuery] = useState("");
  const [activeId, setActiveId] = useState<string | null>(null);
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

  // Keep the same hit when paging prepends matches; a new query starts at the first match.
  useLayoutEffect(() => {
    if (active) { setActiveId(active.id); scroll.reveal(active.key, active.id); }
  }, [active?.id, query, scroll.reveal]);

  const step = (delta: number) => {
    const hit = hits[stepSearchHit(activeIndex, delta, hits.length)];
    if (hit) { setActiveId(hit.id); scroll.reveal(hit.key, hit.id); }
  };
  const loadEarlier = () => {
    scroll.pause();
    void onLoadMore();
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
          {entries.map((entry, index) => <Fragment key={entry.key}>
            {conversationDay(entry.at) !== (index ? conversationDay(entries[index - 1]!.at) : null) && <DaySeparator at={entry.at} />}
            {entry.type === "tools" ? <ToolTimeline items={entry.items} /> : <div data-scroll-anchor={entry.key} data-search-active={active?.key === entry.key || undefined} className={active?.key === entry.key ? "min-w-0 rounded-sm outline-2 outline-offset-4 outline-ring" : "min-w-0"}>
              <FeedEntry item={entry.item} session={session} />
              <SearchMatches hits={matches.get(entry.key) ?? []} activeId={active?.id} />
              <MessageTime at={entry.at} />
            </div>}
          </Fragment>)}
          {activity && <p role="status" data-testid="live-activity" className="flex items-center gap-2 text-xs text-muted-foreground"><LoaderCircle aria-hidden className="size-3.5 animate-spin motion-reduce:animate-none" /><span className="min-w-0 truncate">{activity.label}</span></p>}
        </div>
      </div>
      <SearchTrack hits={hits} activeId={active?.id} />
      {scroll.away && <ScrollAnchor unseen={scroll.unseen} onJump={scroll.jump} />}
    </div>
  </div>;
}
