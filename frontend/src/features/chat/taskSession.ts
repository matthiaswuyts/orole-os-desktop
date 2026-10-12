import { useEffect, useState } from "react";
import { Agents, type AgentsStatus, type Session } from "@/mygo";
import { useAgents } from "@/stores/agents";

/** One request per task id at a time, shared by every chip and pane that names it; settled ones are dropped. */
const requests = new Map<string, Promise<void>>();

function fetchSession(id: string): Promise<void> {
  let request = requests.get(id);
  if (!request) {
    request = Agents.get(id).then((session) => { useAgents.getState().upsert(session); }).finally(() => { requests.delete(id); });
    requests.set(id, request);
  }
  return request;
}

/**
 * A code task as the agents store holds it, kept live by its events. A task the dashboard list has not delivered
 * (or no longer lists) is fetched once; `missing` is true when the daemon does not know it.
 */
export function useTaskSession(id: string): { session: Session | undefined; missing: boolean } {
  const session = useAgents((s) => s.sessions[id]);
  const loaded = useAgents((s) => s.loaded);
  const available = useAgents((s) => s.available);
  const [missing, setMissing] = useState<string | null>(null);
  useEffect(() => {
    if (session || !loaded || !available) return;
    let live = true;
    fetchSession(id).catch(() => { if (live) setMissing(id); });
    return () => { live = false; };
  }, [id, session, loaded, available]);
  return { session, missing: !session && missing === id };
}

/** What a status glyph says about a task, for screen readers and tooltips. */
export const TASK_STATUS_LABEL: Record<AgentsStatus, string> = {
  idle: "Idle", working: "Working", "needs-you": "Needs you", done: "Done", failed: "Failed", stopped: "Stopped", lost: "Lost",
};
