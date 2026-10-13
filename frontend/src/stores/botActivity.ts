import { useShallow } from "zustand/react/shallow";
import type { Session } from "@/mygo";
import { useAgents, type SessionMap } from "./agents";

/** What a bot is doing right now. `idle` is the only state that shows no indicator. */
export type BotActivity = "responding" | "working" | "idle";

/** The ids of bots that own a code task the agent is working on right now (a task that is done, failed, stopped, lost, idle or waiting on the user is not work in progress). */
export function busyBotIds(sessions: SessionMap): string[] {
  const ids = new Set<string>();
  for (const id in sessions) {
    const s: Session = sessions[id]!;
    if (s.status === "working" && s.review !== "discarded" && s.owner?.startsWith("bot:")) ids.add(s.owner.slice("bot:".length));
  }
  return [...ids].sort();
}

/** Replying beats running a task: the reply is what the person is waiting for. */
export const activityOf = (responding: boolean, hasTask: boolean): BotActivity => responding ? "responding" : hasTask ? "working" : "idle";

/** Bots with a code task in progress; compared by value, so unrelated session updates render nothing. */
export function useBusyBotIds(): string[] {
  return useAgents(useShallow((s) => busyBotIds(s.sessions)));
}
