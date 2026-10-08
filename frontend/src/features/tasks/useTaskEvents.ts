import { useCallback, useEffect, useRef, useState } from "react";
import { Agents, events, type Event as AgentEvent } from "@/mygo";
import { mergeEvent, mergeHistory } from "@/lib/taskEvents";
import { errText } from "@/features/agents/format";

/** How many conversation events one history request loads; "Load earlier" asks for another page. */
export const PAGE = 500;

interface State { id: string; list: AgentEvent[]; loading: boolean; loadingMore: boolean; error: string | null; hasMore: boolean }
const initial = (id: string): State => ({ id, list: [], loading: true, loadingMore: false, error: null, hasMore: false });

/**
 * A session's conversation: the newest PAGE events from the daemon plus every event that arrives
 * live, merged by Event.id. Live events that land while history is loading are kept. State carries
 * the session id it belongs to, so switching sessions never shows the previous one's events.
 */
export function useTaskEvents(sessionId: string) {
  const [state, setState] = useState<State>(() => initial(sessionId));
  const limit = useRef(PAGE);
  // Applies an update to this session's state, starting it fresh if the state still belongs to another session.
  const update = useCallback((f: (s: State) => State) => setState((cur) => f(cur.id === sessionId ? cur : initial(sessionId))), [sessionId]);

  useEffect(() => {
    let active = true;
    limit.current = PAGE;
    const off = events.agentsEvent.on((ev) => { if (ev.sessionId === sessionId) update((s) => ({ ...s, list: mergeEvent(s.list, ev) })); });
    Agents.transcript(sessionId, PAGE).then((history) => {
      if (active) update((s) => ({ ...s, list: mergeHistory(history, s.list), hasMore: history.length >= PAGE, loading: false }));
    }, (e: unknown) => {
      if (active) update((s) => ({ ...s, error: errText(e), loading: false }));
    });
    return () => { active = false; off(); };
  }, [sessionId, update]);

  /** Loads another page of older history. Resolves true when events were merged (the caller can keep its scroll anchor). */
  const loadMore = useCallback(async (): Promise<boolean> => {
    const next = limit.current + PAGE;
    update((s) => ({ ...s, loadingMore: true, error: null }));
    try {
      const history = await Agents.transcript(sessionId, next);
      limit.current = next;
      update((s) => ({ ...s, list: mergeHistory(history, s.list), hasMore: history.length >= next, loadingMore: false }));
      return true;
    } catch (e) {
      update((s) => ({ ...s, error: errText(e), loadingMore: false }));
      return false;
    }
  }, [sessionId, update]);

  const view = state.id === sessionId ? state : initial(sessionId);
  return { events: view.list, loading: view.loading, loadingMore: view.loadingMore, error: view.error, hasMore: view.hasMore, loadMore };
}
