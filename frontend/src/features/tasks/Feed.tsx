import { useLayoutEffect, useMemo, useRef, useState } from "react";
import { ArrowDown, LoaderCircle } from "lucide-react";
import type { Event as AgentEvent, Session } from "@/mygo";
import { Button } from "@/components/ui/button";
import { ErrorNote } from "@/features/agents/shared";
import { groupEvents, liveActivity, type FeedItem } from "@/lib/taskEvents";
import { AssistantMessage, ErrorRow, ImageCard, NoteRow, PermissionItem, ReasoningBlock, ToolCard, TurnSeparator, UserMessage } from "./FeedItems";

/** Distance from the bottom, in px, within which the feed keeps following new events. */
const PIN_SLACK = 48;

function FeedEntry({ item, session }: { item: FeedItem; session: Session }) {
  switch (item.type) {
    case "user": return <UserMessage item={item} />;
    case "assistant": return <AssistantMessage item={item} />;
    case "reasoning": return <ReasoningBlock item={item} />;
    case "tool": return <ToolCard call={item.call} />;
    case "image": return <ImageCard sessionId={session.id} item={item} />;
    case "permission": return <PermissionItem session={session} item={item} />;
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
}

/** The conversation: grouped event items, pinned to the newest unless the user scrolled up. */
export function Feed({ session, events, loading, loadingMore, error, hasMore, onLoadMore }: FeedProps) {
  const items = useMemo(() => groupEvents(events, { live: session.live }), [events, session.live]);
  const activity = liveActivity(session);
  const scroller = useRef<HTMLDivElement>(null);
  const pinned = useRef(true);
  const anchor = useRef<{ height: number; top: number } | null>(null);
  const [away, setAway] = useState(false);

  useLayoutEffect(() => {
    const el = scroller.current;
    if (!el) return;
    if (anchor.current) { // older history was prepended: keep the reader where they were
      el.scrollTop = anchor.current.top + (el.scrollHeight - anchor.current.height);
      anchor.current = null;
    } else if (pinned.current) el.scrollTop = el.scrollHeight;
  }, [items, activity?.label]);

  const onScroll = () => {
    const el = scroller.current;
    if (!el) return;
    pinned.current = el.scrollHeight - el.scrollTop - el.clientHeight < PIN_SLACK;
    setAway(!pinned.current);
  };
  const jump = () => {
    const el = scroller.current;
    if (!el) return;
    pinned.current = true;
    el.scrollTop = el.scrollHeight;
    setAway(false);
  };
  const loadEarlier = () => {
    const el = scroller.current;
    if (el) anchor.current = { height: el.scrollHeight, top: el.scrollTop };
    void onLoadMore().then((ok) => { if (!ok) anchor.current = null; });
  };

  return <div className="relative flex min-h-0 flex-1 flex-col">
    <div ref={scroller} onScroll={onScroll} role="log" tabIndex={0} aria-label="Task conversation" aria-busy={loading} className="min-h-0 flex-1 overflow-auto px-5 py-4 outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring">
      <div className="mx-auto flex max-w-4xl flex-col gap-3">
        <ErrorNote error={error} />
        {hasMore && <Button size="xs" variant="outline" className="self-center" loading={loadingMore} onClick={loadEarlier}>Load earlier messages</Button>}
        {loading && <p role="status" className="text-muted-foreground">Loading conversation…</p>}
        {!loading && !items.length && <p role="status" className="text-muted-foreground">No output has been recorded yet.</p>}
        {items.map((item) => <FeedEntry key={item.key} item={item} session={session} />)}
        {activity && <p role="status" data-testid="live-activity" className="flex items-center gap-2 text-xs text-muted-foreground"><LoaderCircle aria-hidden className="size-3.5 animate-spin motion-reduce:animate-none" /><span className="min-w-0 truncate">{activity.label}</span></p>}
      </div>
    </div>
    {away && <Button size="xs" variant="outline" className="absolute bottom-3 left-1/2 -translate-x-1/2 shadow-md" onClick={jump}><ArrowDown data-icon="inline-start" />Jump to latest</Button>}
  </div>;
}
