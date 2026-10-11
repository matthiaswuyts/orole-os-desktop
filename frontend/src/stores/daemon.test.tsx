import { act, cleanup, render } from "@testing-library/react";
import { Profiler } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Health } from "@/mygo";

const bridge = vi.hoisted(() => ({ health: vi.fn(), state: null as ((l: { connected: boolean; error?: string }) => void) | null }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => {
  const unsupported = new Proxy({}, { get: () => async () => { throw new Error("unused"); } });
  return {
    Daemon: { health: bridge.health }, Machines: unsupported, MCP: unsupported, Agents: unsupported, Updates: unsupported,
    events: new Proxy({}, { get: (_, name) => ({ on: (f: never) => { if (name === "daemonState") bridge.state = f; return () => {}; } }) }),
  };
});

import { StatusBar } from "@/components/shell/StatusBar";
import { startDaemonPolling, useDaemon } from "@/stores/daemon";

const health = (over: Partial<Health> = {}): Health => ({ version: "1.0.0", pid: 1, startedAt: "t", uptimeSec: 10, dbVersion: 1, platform: "windows", goVersion: "go", goroutines: 5, memAllocMB: 3, terminals: 0, protocolVersion: 1, ...over });
const settle = async () => { for (let i = 0; i < 10; i++) await Promise.resolve(); };
const poll = async (times: number) => { for (let i = 0; i < times; i++) { await useDaemon.getState().refresh(); } };

beforeEach(() => {
  bridge.health.mockReset();
  useDaemon.setState({ link: "unknown", health: null, error: null, refreshedAt: 0, connectEpoch: 0 });
});
afterEach(cleanup);

describe("daemon health polling", () => {
  it("notifies subscribers only when the observation changed, not on every identical poll", async () => {
    bridge.health.mockResolvedValue(health());
    const seen = vi.fn();
    const off = useDaemon.subscribe(seen);
    await poll(20);
    expect(seen).toHaveBeenCalledTimes(1);
    expect(useDaemon.getState()).toMatchObject({ link: "connected", error: null, connectEpoch: 1 });
    bridge.health.mockResolvedValue(health({ uptimeSec: 15 }));
    await poll(3);
    expect(seen).toHaveBeenCalledTimes(2);
    off();
  });

  it("stays quiet while offline with the same error, and says so again when the error changes", async () => {
    bridge.health.mockRejectedValue(new Error("pipe closed"));
    const seen = vi.fn();
    const off = useDaemon.subscribe(seen);
    await poll(10);
    bridge.state?.({ connected: false, error: "pipe closed" });
    expect(seen).toHaveBeenCalledTimes(1);
    expect(useDaemon.getState()).toMatchObject({ link: "offline", error: "pipe closed" });
    bridge.health.mockRejectedValue(new Error("timeout"));
    await poll(2);
    expect(seen).toHaveBeenCalledTimes(2);
    off();
  });

  it("an offline daemonState event after an offline poll with the same error is a no-op, and recovery bumps connectEpoch once", async () => {
    bridge.health.mockRejectedValue(new Error("gone"));
    const stop = startDaemonPolling();
    await settle();
    const seen = vi.fn();
    const off = useDaemon.subscribe(seen);
    bridge.state!({ connected: false, error: "gone" });
    expect(seen).not.toHaveBeenCalled();
    bridge.health.mockResolvedValue(health());
    await poll(3);
    expect(useDaemon.getState().connectEpoch).toBe(1);
    off();
    stop();
  });
});

describe("StatusBar subscriptions", () => {
  it("does not re-render on health polls that change only live metrics", async () => {
    bridge.health.mockResolvedValue(health());
    await poll(1);
    let commits = 0;
    render(<Profiler id="status" onRender={() => { commits++; }}><StatusBar /></Profiler>);
    await act(settle);
    commits = 0;
    for (let uptime = 20; uptime < 70; uptime += 5) {
      bridge.health.mockResolvedValue(health({ uptimeSec: uptime, goroutines: uptime, memAllocMB: uptime / 2 }));
      await act(() => useDaemon.getState().refresh());
    }
    expect(commits).toBe(0);
    bridge.health.mockResolvedValue(health({ version: "1.1.0" }));
    await act(() => useDaemon.getState().refresh());
    expect(commits).toBe(1);
  });
});
