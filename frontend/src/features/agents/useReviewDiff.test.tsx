import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReviewDiff } from "@/mygo";

const api = vi.hoisted(() => ({ diff: vi.fn() }));
vi.mock("@/mygo", () => ({ Agents: { diff: api.diff } }));
import { DIFF_REFRESH_MS, useReviewDiff } from "./useReviewDiff";

const review = (sessionId: string, files = 0): ReviewDiff => ({ sessionId, files, additions: 0, deletions: 0, truncated: false, error: "", diff: { files: [], truncated: false } }) as unknown as ReviewDiff;
const stamp = (n: number) => new Date(Date.UTC(2026, 9, 8, 12, 0, n)).toISOString();
const flush = () => act(async () => { await Promise.resolve(); await Promise.resolve(); });

beforeEach(() => {
  vi.useFakeTimers({ now: 0 });
  api.diff.mockReset();
  api.diff.mockImplementation((id: string) => Promise.resolve(review(id)));
});
afterEach(() => { cleanup(); vi.useRealTimers(); });

describe("useReviewDiff", () => {
  it("loads once on mount and not at all while disabled", async () => {
    const off = renderHook(() => useReviewDiff("s1", stamp(0), false));
    await flush();
    expect(api.diff).not.toHaveBeenCalled();
    expect(off.result.current.diff).toBeNull();
    const on = renderHook(() => useReviewDiff("s1", stamp(0), true));
    await flush();
    expect(api.diff).toHaveBeenCalledTimes(1);
    expect(on.result.current.diff?.sessionId).toBe("s1");
  });

  it("collapses a burst of session updates into one trailing refresh per interval", async () => {
    const { result, rerender } = renderHook(({ at }) => useReviewDiff("s1", at, true), { initialProps: { at: stamp(0) } });
    await flush();
    expect(api.diff).toHaveBeenCalledTimes(1);
    for (let i = 1; i <= 40; i++) {
      rerender({ at: stamp(i) });
      act(() => { vi.advanceTimersByTime(25); }); // 40 updates over one second
    }
    await flush();
    expect(api.diff).toHaveBeenCalledTimes(1);
    api.diff.mockImplementation((id: string) => Promise.resolve(review(id, 7)));
    await act(async () => { vi.advanceTimersByTime(DIFF_REFRESH_MS); await Promise.resolve(); await Promise.resolve(); });
    expect(api.diff).toHaveBeenCalledTimes(2);
    expect(result.current.diff?.files).toBe(7);
    await act(async () => { vi.advanceTimersByTime(DIFF_REFRESH_MS * 3); });
    expect(api.diff).toHaveBeenCalledTimes(2);
  });

  it("never overlaps requests: an update during a slow diff waits for it, then refreshes once", async () => {
    let finish!: (d: ReviewDiff) => void;
    api.diff.mockImplementationOnce(() => new Promise<ReviewDiff>((resolve) => { finish = resolve; }));
    const { rerender } = renderHook(({ at }) => useReviewDiff("s1", at, true), { initialProps: { at: stamp(0) } });
    await flush();
    rerender({ at: stamp(1) });
    act(() => { vi.advanceTimersByTime(DIFF_REFRESH_MS * 5); });
    expect(api.diff).toHaveBeenCalledTimes(1);
    await act(async () => { finish(review("s1")); await Promise.resolve(); await Promise.resolve(); });
    await act(async () => { vi.advanceTimersByTime(DIFF_REFRESH_MS); await Promise.resolve(); });
    expect(api.diff).toHaveBeenCalledTimes(2);
  });

  it("keeps the previous diff while refreshing, shows an error, and drops a result when another session is shown", async () => {
    const { result, rerender } = renderHook(({ id, at }) => useReviewDiff(id, at, true), { initialProps: { id: "s1", at: stamp(0) } });
    await flush();
    expect(result.current.diff?.sessionId).toBe("s1");
    let finish!: (d: ReviewDiff) => void;
    api.diff.mockImplementationOnce(() => new Promise<ReviewDiff>((resolve) => { finish = resolve; }));
    rerender({ id: "s1", at: stamp(1) });
    await act(async () => { vi.advanceTimersByTime(DIFF_REFRESH_MS); });
    expect(result.current.diff?.sessionId).toBe("s1");
    await act(async () => { finish(review("s1", 2)); await Promise.resolve(); await Promise.resolve(); });
    expect(result.current.diff?.files).toBe(2);
    api.diff.mockRejectedValueOnce(new Error("worktree missing"));
    rerender({ id: "s2", at: stamp(2) });
    expect(result.current.diff).toBeNull();
    await flush();
    expect(api.diff).toHaveBeenLastCalledWith("s2");
    expect(result.current.error).toBe("worktree missing");
    await act(async () => { result.current.retry(); await Promise.resolve(); await Promise.resolve(); });
    expect(result.current.error).toBeNull();
    expect(result.current.diff?.sessionId).toBe("s2");
  });
});
