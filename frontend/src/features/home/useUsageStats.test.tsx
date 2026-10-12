import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { UsageQuery, UsageStats } from "@/mygo";

const api = vi.hoisted(() => ({ usage: vi.fn() }));
vi.mock("@/mygo", () => ({ Agents: { usage: api.usage }, events: { daemonState: { on: () => () => {} } } }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));

const reply = (q: UsageQuery, total = 1) => ({ byAgent: [{ kind: q.byAgentRange, sessions: 1, tokens: { total } }] }) as unknown as UsageStats;
const flush = () => act(async () => { await Promise.resolve(); await Promise.resolve(); await Promise.resolve(); });

// The cache and throttle live at module level, so every test starts from a fresh module.
async function load() {
  vi.resetModules();
  const [usage, agents] = await Promise.all([import("./useUsageStats"), import("@/stores/agents")]);
  return { ...usage, useAgents: agents.useAgents };
}

beforeEach(() => {
  vi.useFakeTimers({ now: Date.UTC(2026, 9, 12, 12) });
  api.usage.mockReset();
  api.usage.mockImplementation((q: UsageQuery) => Promise.resolve(reply(q)));
});
afterEach(() => { cleanup(); vi.useRealTimers(); });

describe("useUsageStats", () => {
  it("shares one request between widgets and reuses a fresh reply on remount", async () => {
    const { useUsageStats } = await load();
    const a = renderHook(() => useUsageStats());
    const b = renderHook(() => useUsageStats());
    expect(a.result.current.status).toBe("loading");
    await flush();
    expect(api.usage).toHaveBeenCalledTimes(1);
    expect(api.usage).toHaveBeenCalledWith({ byAgentRange: "24h", latest: 12 });
    expect(b.result.current.status).toBe("ready");
    a.unmount(); b.unmount();
    act(() => { vi.advanceTimersByTime(5_000); });
    const again = renderHook(() => useUsageStats());
    expect(again.result.current.stats).not.toBeNull();
    await flush();
    expect(api.usage).toHaveBeenCalledTimes(1);
  });

  it("collapses a burst of session events into one refresh at the end of the 15 s gap", async () => {
    const { useUsageStats, useAgents, MIN_GAP_MS } = await load();
    const view = renderHook(() => useUsageStats());
    await flush();
    expect(api.usage).toHaveBeenCalledTimes(1);
    api.usage.mockImplementation((q: UsageQuery) => Promise.resolve(reply(q, 7)));
    for (let i = 0; i < 40; i++) {
      act(() => { useAgents.setState({ sessions: {} }); vi.advanceTimersByTime(25); });
    }
    expect(api.usage).toHaveBeenCalledTimes(1);
    await act(async () => { vi.advanceTimersByTime(MIN_GAP_MS); await Promise.resolve(); await Promise.resolve(); });
    expect(api.usage).toHaveBeenCalledTimes(2);
    expect(view.result.current.stats?.byAgent[0]?.tokens.total).toBe(7);
  });

  it("queues one follow-up when events arrive while a request is in flight", async () => {
    const { useUsageStats, useAgents, MIN_GAP_MS } = await load();
    let finish!: (stats: UsageStats) => void;
    api.usage.mockImplementationOnce(() => new Promise<UsageStats>((resolve) => { finish = resolve; }));
    renderHook(() => useUsageStats());
    act(() => { vi.advanceTimersByTime(MIN_GAP_MS * 2); });
    act(() => { useAgents.setState({ sessions: {} }); useAgents.setState({ sessions: {} }); });
    expect(api.usage).toHaveBeenCalledTimes(1);
    await act(async () => { finish(reply({ byAgentRange: "24h", latest: 12 })); await Promise.resolve(); await Promise.resolve(); await Promise.resolve(); });
    expect(api.usage).toHaveBeenCalledTimes(2);
  });

  it("asks for each By agent range separately and reports a failure with a retry", async () => {
    const { useUsageStats } = await load();
    api.usage.mockRejectedValueOnce(new Error("daemon busy"));
    const week = renderHook(() => useUsageStats("7d"));
    await flush();
    expect(api.usage).toHaveBeenCalledWith({ byAgentRange: "7d", latest: 12 });
    expect(week.result.current).toMatchObject({ status: "error", message: "daemon busy" });
    week.result.current.retry?.();
    await flush();
    expect(api.usage).toHaveBeenCalledTimes(2);
    expect(week.result.current.status).toBe("ready");
    expect(week.result.current.stats?.byAgent[0]?.kind).toBe("7d");
  });
});
