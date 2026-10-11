import { describe, expect, it, vi } from "vitest";
import type { Session, AgentsStatus } from "@/mygo";
import { columnOf, groupColumns } from "@/features/agents/columns";
import { reviewList } from "@/features/review/filter";
vi.mock("@/lib/backend", () => ({ inDesktop: false }));
vi.mock("@/mygo", () => ({ Agents: {}, events: {} }));
import { applySessionDelta, applySessionUpdate, removeSession, selectCounts, mergeList } from "./agents";
const session = (id: string, status: AgentsStatus, review = "", updatedAt = "2026-10-08T10:00:00Z") => ({ id, status, review, updatedAt }) as Session;

describe("dashboard columns", () => {
  it.each([["needs-you", "needs-you"], ["failed", "needs-you"], ["working", "working"], ["done", "done"], ["idle", "idle"], ["stopped", "idle"], ["lost", "idle"]] as const)("groups %s as %s", (status, column) => { expect(columnOf(status)).toBe(column); });
  it("hides discarded, retains approved, sorts newest first", () => {
    const out = groupColumns([session("old", "done"), session("hidden", "done", "discarded"), session("approved", "done", "approved", "2026-10-08T11:00:00Z")]);
    expect(out.done.map((s) => s.id)).toEqual(["approved", "old"]);
    expect(out["needs-you"]).toEqual([]);
  });
});
describe("agent reducers", () => {
  it("upserts without mutating and rejects stale events", () => {
    const a = session("a", "idle"); const first = { a };
    const updated = applySessionUpdate(first, session("a", "working", "", "2026-10-08T11:00:00Z"));
    expect(first.a.status).toBe("idle"); expect(updated.a.status).toBe("working");
    expect(applySessionUpdate(updated, a)).toBe(updated);
    expect(Object.keys(applySessionUpdate(updated, session("b", "done")))).toEqual(["a", "b"]);
  });
  it("removes a session without changing the input", () => {
    const first = { a: session("a", "done") }; expect(removeSession(first, "a")).toEqual({}); expect(first.a).toBeDefined(); expect(removeSession(first, "absent")).toBe(first);
  });
  it("keeps additions and removals received during a list", () => {
    const held = { a: session("a", "working"), b: session("b", "done") };
    const result = mergeList(held, [session("a", "idle", "", "2026-10-08T09:00:00Z"), session("removed", "done")], new Set(["b", "removed"]));
    expect(result).toEqual(held);
  });
  it("merges a delta into the held session without touching other fields", () => {
    const held = { d1: { ...session("d1", "working"), activity: "old", tail: "t", files: 1 } as Session };
    const next = applySessionDelta(held, { id: "d1", updatedAt: "2026-10-08T10:00:05Z", activity: "new", files: 3 });
    expect(next.d1).toMatchObject({ status: "working", activity: "new", tail: "t", files: 3, updatedAt: "2026-10-08T10:00:05Z" });
    expect(held.d1.activity).toBe("old");
    expect(applySessionDelta(held, { id: "unknown", updatedAt: "2026-10-08T10:00:05Z", activity: "x" })).toBe(held);
    expect(applySessionDelta(next, { id: "d1", updatedAt: "2026-10-08T10:00:01Z", activity: "stale" })).toBe(next);
  });
  it("keeps a status change that arrives after a newer delta, and the delta's display fields", () => {
    const held = applySessionUpdate({}, { ...session("d2", "working"), activity: "a" } as Session);
    const afterDelta = applySessionDelta(held, { id: "d2", updatedAt: "2026-10-08T10:00:05Z", activity: "from delta" });
    const full = { ...session("d2", "needs-you", "", "2026-10-08T10:00:03Z"), activity: "from full" } as Session;
    const merged = applySessionUpdate(afterDelta, full);
    expect(merged.d2).toMatchObject({ status: "needs-you", activity: "from delta", updatedAt: "2026-10-08T10:00:05Z" });
    expect(applySessionUpdate(merged, { ...full, status: "working", updatedAt: "2026-10-08T10:00:02Z" } as Session)).toBe(merged);
  });
  it("counts failed as needs-you and only unreviewed finished turns as reviewable", () => {
    const sessions = [session("a", "needs-you"), session("b", "failed"), session("c", "working"), session("d", "done"), session("e", "lost"), session("f", "done", "approved"), session("g", "failed", "discarded"), session("h", "working", "sent-back")];
    expect(selectCounts(Object.fromEntries(sessions.map((s) => [s.id, s])))).toEqual({ needsYou: 2, working: 2, done: 2, idle: 1, total: 7, reviewable: 2 });
  });
});
describe("review list", () => {
  it("includes done/failed and any recorded decision, excludes discarded by default", () => {
    const all = [session("done", "done"), session("failed", "failed"), session("working", "working"), session("sent-back", "working", "sent-back"), session("approved", "done", "approved"), session("discarded", "stopped", "discarded")];
    expect(reviewList(all).map((s) => s.id)).toEqual(["done", "failed", "sent-back", "approved"]);
    expect(reviewList(all, true).map((s) => s.id)).toContain("discarded");
  });
  it("orders by update descending", () => { expect(reviewList([session("old", "done"), session("new", "failed", "", "2026-10-08T12:00:00Z")]).map((s) => s.id)).toEqual(["new", "old"]); });
});
