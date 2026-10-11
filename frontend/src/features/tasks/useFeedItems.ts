import { useState } from "react";
import type { Event as AgentEvent } from "@/mygo";
import { groupEvents, shareItems, type FeedItem } from "@/lib/taskEvents";

interface Grouped { events: AgentEvent[]; live: boolean; items: FeedItem[] }

/**
 * The feed items of a session's events, grouped once per change for every consumer (the feed and the
 * task canvas). Items an event did not touch keep their identity, so memoized rows skip rendering and
 * an event that changes nothing visible (usage) returns the previous array.
 */
export function useFeedItems(events: AgentEvent[], live: boolean): FeedItem[] {
  const [grouped, setGrouped] = useState<Grouped>(() => ({ events, live, items: groupEvents(events, { live }) }));
  if (grouped.events === events && grouped.live === live) return grouped.items;
  // Derived during render from the previous result: React re-renders this component at once, without committing the stale items.
  const items = shareItems(grouped.items, groupEvents(events, { live }));
  setGrouped({ events, live, items });
  return items;
}
