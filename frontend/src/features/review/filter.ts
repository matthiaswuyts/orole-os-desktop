import type { Session } from "@/mygo";

/** Finished sessions and recorded review decisions; discarded work is opt-in. */
export function reviewList(sessions: Iterable<Session>, showDiscarded = false): Session[] {
  return [...sessions].filter((s) => (showDiscarded || s.review !== "discarded") && (s.status === "done" || s.status === "failed" || s.review !== "")).sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt));
}
