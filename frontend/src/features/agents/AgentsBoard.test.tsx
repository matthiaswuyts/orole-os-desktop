import { act, cleanup, render, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Agents, type Session } from "@/mygo";
import type * as SharedModule from "./shared";

const renders = vi.hoisted(() => ({ glyph: 0 }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("./shared", async (original) => {
  const actual = await original<typeof SharedModule>();
  return { ...actual, AgentGlyph: (props: Parameters<typeof actual.AgentGlyph>[0]) => { renders.glyph++; return actual.AgentGlyph(props); } };
});
import { useAgents } from "@/stores/agents";
import { AgentsView } from "./AgentsView";
import { sessionDuration, useNow } from "./format";

const at = "2026-10-08T12:00:00Z";
const session = (n: number, patch: Partial<Session> = {}): Session => ({
  id: `s${n}`, title: `Task ${n}`, kind: "claude", machine: "local", project: "C:\\code\\p", dir: "C:\\code\\p", branch: "main", model: "sonnet", mode: "structured",
  status: n % 4 === 0 ? "working" : n % 4 === 1 ? "done" : n % 4 === 2 ? "needs-you" : "idle", live: n % 4 === 0, activity: "", tail: "", review: "", pending: [],
  startedAt: at, updatedAt: new Date(Date.parse(at) + n * 1000).toISOString(), ...patch,
}) as Session;

beforeEach(() => {
  renders.glyph = 0;
  vi.spyOn(Agents, "detect").mockResolvedValue([]);
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.useRealTimers(); });

describe("agents board", () => {
  it("re-renders only the card of the session that changed while 100 updates flood a 200-session board", async () => {
    const sessions = Object.fromEntries(Array.from({ length: 200 }, (_, n) => [`s${n}`, session(n)]));
    useAgents.setState({ available: true, loaded: true, loading: false, offline: false, error: null, sessions, runs: [], projectFilter: null, openId: null });
    render(<AgentsView />);
    await act(async () => { await Promise.resolve(); });
    expect(renders.glyph).toBe(200);
    renders.glyph = 0;
    const started = performance.now();
    for (let i = 0; i < 100; i++) {
      // The session's activity changes; its position (updatedAt) stays inside the column, as an event stream does.
      act(() => useAgents.getState().upsert(session(i % 200, { activity: `step ${i}`, updatedAt: new Date(Date.parse(at) + 1e6 + i * 1000).toISOString() })));
    }
    console.log("BOARD", JSON.stringify({ cardRenders: renders.glyph, ms: Math.round(performance.now() - started) }));
    // origin/main re-rendered all 200 cards on every update (20000 for this loop).
    expect(renders.glyph).toBe(100);
    // The assertion counts renders, not time; a loaded CI runner needs more than the default 5 s to mount 200 cards.
  }, 30_000);
});

describe("session clock", () => {
  it("ticks only for live sessions", () => {
    vi.useFakeTimers({ now: Date.parse(at) });
    const live = renderHook(() => useNow(true));
    const stopped = renderHook(() => useNow(false));
    const before = [live.result.current, stopped.result.current];
    act(() => { vi.advanceTimersByTime(3000); });
    expect(live.result.current).toBe(before[0]! + 3000);
    expect(stopped.result.current).toBe(before[1]);
    expect(vi.getTimerCount()).toBe(1);
    live.unmount();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("measures a finished session up to when it ended and a live one up to now", () => {
    const base = { startedAt: at, updatedAt: "2026-10-08T12:10:00Z" };
    const now = Date.parse(at) + 3_600_000;
    expect(sessionDuration({ ...base, live: false }, now)).toBe("10m");
    expect(sessionDuration({ ...base, live: false, endedAt: "2026-10-08T12:03:00Z" }, now)).toBe("3m");
    expect(sessionDuration({ ...base, live: true }, now)).toBe("1h 00m");
  });
});
