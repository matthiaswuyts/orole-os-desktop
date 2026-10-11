import { useState } from "react";
import type { Event as AgentEvent, Session } from "@/mygo";
import { groupEvents, groupRuns, shareEntries, shareItems, type FeedItem, type RunContext, type TurnEntry } from "@/lib/taskEvents";

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

interface Turns { items: readonly FeedItem[]; running: boolean; live: boolean; pending: Session["pending"]; questions: Session["questions"]; entries: TurnEntry[] }

const buildEntries = (items: readonly FeedItem[], running: boolean, live: boolean, pending: Session["pending"], questions: Session["questions"]): TurnEntry[] => {
  const context: RunContext = { running, live, pendingPermissions: new Set(pending?.map((request) => request.id)), pendingQuestions: new Set(questions?.map((request) => request.id)) };
  return groupRuns(items, context);
};

/**
 * One entry per user message and per assistant turn, regrouped when the items or the session's pending requests change.
 * Entries an update did not touch keep their identity, so memoized rows of a long conversation skip rendering.
 */
export function useTurnEntries(items: readonly FeedItem[], running: boolean, live: boolean, pending: Session["pending"], questions: Session["questions"]): TurnEntry[] {
  const [turns, setTurns] = useState<Turns>(() => ({ items, running, live, pending, questions, entries: buildEntries(items, running, live, pending, questions) }));
  if (turns.items === items && turns.running === running && turns.live === live && turns.pending === pending && turns.questions === questions) return turns.entries;
  const entries = shareEntries(turns.entries, buildEntries(items, running, live, pending, questions));
  setTurns({ items, running, live, pending, questions, entries });
  return entries;
}
