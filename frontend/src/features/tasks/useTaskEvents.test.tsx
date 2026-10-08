import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Event as AgentEvent } from "@/mygo";

const api = vi.hoisted(() => ({ transcript: vi.fn(), listeners: new Set<(e: AgentEvent) => void>() }));
vi.mock("@/mygo", () => ({ Agents: { transcript: api.transcript }, events: { agentsEvent: { on: (f: (e: AgentEvent) => void) => { api.listeners.add(f); return () => api.listeners.delete(f); } } } }));
import { PAGE, useTaskEvents } from "./useTaskEvents";

const ev = (id: number, sessionId = "s1", kind = "message"): AgentEvent => ({ id, sessionId, at: "2026-10-08T12:00:00Z", kind, text: `m${id}`, data: null });
const emit = (e: AgentEvent) => act(() => { for (const f of api.listeners) f(e); });

beforeEach(() => { vi.clearAllMocks(); api.listeners.clear(); });
afterEach(cleanup);

describe("useTaskEvents", () => {
  it("merges live events that arrive while history loads, by id, ignoring other sessions and non-feed kinds", async () => {
    let resolve!: (h: AgentEvent[]) => void;
    api.transcript.mockReturnValue(new Promise<AgentEvent[]>((r) => { resolve = r; }));
    const { result } = renderHook(() => useTaskEvents("s1"));
    expect(result.current.loading).toBe(true);
    emit(ev(3)); emit(ev(9, "other")); emit(ev(4, "s1", "activity"));
    await act(async () => { resolve([ev(1), ev(2), ev(3)]); await Promise.resolve(); });
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.events.map((e) => e.id)).toEqual([1, 2, 3]);
    emit(ev(5));
    expect(result.current.events.map((e) => e.id)).toEqual([1, 2, 3, 5]);
    expect(result.current.hasMore).toBe(false);
  });

  it("offers older history when a full page came back and loads the next page", async () => {
    const page = Array.from({ length: PAGE }, (_, i) => ev(i + 1000));
    api.transcript.mockResolvedValueOnce(page);
    const { result } = renderHook(() => useTaskEvents("s1"));
    await waitFor(() => expect(result.current.hasMore).toBe(true));
    api.transcript.mockResolvedValueOnce([ev(1), ev(2), ...page]);
    let ok = false;
    await act(async () => { ok = await result.current.loadMore(); });
    expect(ok).toBe(true);
    expect(api.transcript).toHaveBeenLastCalledWith("s1", PAGE * 2);
    expect(result.current.events[0]!.id).toBe(1);
    expect(result.current.hasMore).toBe(false);
  });

  it("surfaces a history error and still shows live events", async () => {
    api.transcript.mockRejectedValue(new Error("daemon offline"));
    const { result } = renderHook(() => useTaskEvents("s1"));
    await waitFor(() => expect(result.current.error).toBe("daemon offline"));
    emit(ev(1));
    expect(result.current.events).toHaveLength(1);
  });

  it("does not show one session's events under another", async () => {
    api.transcript.mockResolvedValue([ev(1)]);
    const { result, rerender } = renderHook(({ id }) => useTaskEvents(id), { initialProps: { id: "s1" } });
    await waitFor(() => expect(result.current.events).toHaveLength(1));
    api.transcript.mockResolvedValue([ev(7, "s2")]);
    rerender({ id: "s2" });
    expect(result.current.events).toHaveLength(0);
    await waitFor(() => expect(result.current.events.map((e) => e.id)).toEqual([7]));
  });
});
