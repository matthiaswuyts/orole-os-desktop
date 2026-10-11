import type { AgentsStatus, Session } from "@/mygo";

export type ColumnId = "needs-you" | "working" | "done" | "idle";

export interface ColumnSpec {
  id: ColumnId;
  title: string;
  /** Shown in an empty column. */
  empty: string;
}

export const COLUMN_SPECS: readonly ColumnSpec[] = [
  { id: "needs-you", title: "Needs you", empty: "Nothing needs you" },
  { id: "working", title: "Working", empty: "No agent is working" },
  { id: "done", title: "Done", empty: "Finished turns land here" },
  { id: "idle", title: "Idle", empty: "No idle agents" },
] as const;

/**
 * Dashboard column of a status. failed joins "Needs you" (it is a turn that went wrong);
 * stopped and lost sit in Idle with a muted badge.
 */
export function columnOf(status: AgentsStatus): ColumnId {
  switch (status) {
    case "needs-you":
    case "failed":
      return "needs-you";
    case "working":
      return "working";
    case "done":
      return "done";
    default:
      return "idle";
  }
}

/** Discarded sessions are gone from the dashboard; approved ones stay in Done. */
export const onDashboard = (s: Session) => s.review !== "discarded";

export type Columns = Record<ColumnId, Session[]>;

/** Groups sessions into dashboard columns, each sorted by updatedAt descending (each timestamp parsed once). */
export function groupColumns(sessions: Iterable<Session>): Columns {
  const stamped: Record<ColumnId, { at: number; session: Session }[]> = { "needs-you": [], working: [], done: [], idle: [] };
  for (const s of sessions) if (onDashboard(s)) stamped[columnOf(s.status)].push({ at: Date.parse(s.updatedAt) || 0, session: s });
  const out: Columns = { "needs-you": [], working: [], done: [], idle: [] };
  for (const k of Object.keys(out) as ColumnId[]) out[k] = stamped[k].sort((a, b) => b.at - a.at).map((entry) => entry.session);
  return out;
}
