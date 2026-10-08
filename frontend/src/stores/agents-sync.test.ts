import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Session } from "@/mygo";

const bridge = vi.hoisted(() => {
  const callbacks = new Map<string, Set<(value: unknown) => void>>();
  const event = (name: string) => ({ on: (callback: (value: unknown) => void) => { const set = callbacks.get(name) ?? new Set(); set.add(callback); callbacks.set(name, set); return () => { set.delete(callback); }; } });
  return { callbacks, list: vi.fn(async (): Promise<Session[]> => []), runs: vi.fn(async () => []), events: { agentsUpdated: event("updated"), agentsRemoved: event("removed"), agentsRun: event("run"), daemonState: event("daemon") } };
});
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({ Agents: { list: bridge.list, runs: bridge.runs }, events: bridge.events }));

beforeEach(() => { vi.resetModules(); bridge.callbacks.clear(); bridge.list.mockReset(); bridge.list.mockResolvedValue([]); bridge.runs.mockClear(); });

describe("agents synchronization", () => {
  it("shares subscriptions, disposes idempotently and reconnects after StrictMode cleanup", async () => {
    const { useAgents } = await import("./agents");
    const off1 = useAgents.getState().init(); const off2 = useAgents.getState().init();
    expect(bridge.callbacks.get("updated")?.size).toBe(1);
    off1(); off1(); expect(bridge.callbacks.get("updated")?.size).toBe(1);
    off2(); expect(bridge.callbacks.get("updated")?.size).toBe(0);
    const off3 = useAgents.getState().init(); expect(bridge.callbacks.get("updated")?.size).toBe(1); off3();
    await vi.waitFor(() => expect(useAgents.getState().loading).toBe(false));
  });
  it("does not resurrect a session removed while the initial list is loading", async () => {
    let resolveList!: (sessions: Session[]) => void;
    bridge.list.mockImplementationOnce(() => new Promise<Session[]>((resolve) => { resolveList = resolve; }));
    const { useAgents } = await import("./agents"); const off = useAgents.getState().init();
    bridge.callbacks.get("removed")?.forEach((callback) => callback({ id: "removed" }));
    resolveList([{ id: "removed", status: "done", review: "", updatedAt: "2026-10-08T10:00:00Z" } as Session]);
    await vi.waitFor(() => expect(useAgents.getState().loaded).toBe(true));
    expect(useAgents.getState().sessions).toEqual({}); off();
  });
});
