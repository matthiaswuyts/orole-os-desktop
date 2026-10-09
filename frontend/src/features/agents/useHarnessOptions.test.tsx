import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { LaunchOptions } from "@/mygo";

const api = vi.hoisted(() => ({ options: vi.fn() }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({ Agents: api }));
import { useDaemon } from "@/stores/daemon";
import { useHarnessOptionsCache } from "@/stores/harnessOptions";
import { useHarnessOptions } from "./useHarnessOptions";

const options = (machine: string): LaunchOptions => ({ machine, projects: [], directories: [], harnesses: [], worktree: true, unavailableReason: "" });
beforeEach(() => {
  vi.clearAllMocks();
  useDaemon.setState({ link: "connected", connectEpoch: 0 });
  useHarnessOptionsCache.setState({ machines: {} });
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it("shares in-flight discovery and returns cached choices synchronously on reopening", async () => {
  const pending = Promise.withResolvers<LaunchOptions>();
  api.options.mockReturnValue(pending.promise);
  const home = renderHook(() => useHarnessOptions("local"));
  const dialog = renderHook(() => useHarnessOptions("local"));
  expect(api.options).toHaveBeenCalledTimes(1);
  const discovered = options("local");
  await act(async () => { pending.resolve(discovered); await pending.promise; });
  expect(home.result.current.options).toBe(discovered);
  dialog.unmount(); home.unmount();
  const reopened = renderHook(() => useHarnessOptions("local"));
  expect(reopened.result.current.options).toBe(discovered);
  expect(reopened.result.current.loading).toBe(false);
  expect(api.options).toHaveBeenCalledTimes(1);
});

it("keeps cached choices visible during a stale refresh and reports refresh failures", async () => {
  const clock = vi.spyOn(Date, "now").mockReturnValue(100_000);
  api.options.mockResolvedValue(options("local"));
  const first = renderHook(() => useHarnessOptions("local"));
  await waitFor(() => expect(first.result.current.options).toBeDefined());
  const cached = first.result.current.options;
  first.unmount(); clock.mockReturnValue(150_000);
  const pending = Promise.withResolvers<LaunchOptions>();
  api.options.mockReturnValue(pending.promise);
  const reopened = renderHook(() => useHarnessOptions("local"));
  expect(reopened.result.current.options).toBe(cached);
  expect(reopened.result.current.loading).toBe(false);
  expect(api.options).toHaveBeenCalledTimes(2);
  await act(async () => { pending.reject(new Error("Remote discovery failed")); await pending.promise.catch(() => {}); });
  expect(reopened.result.current.options).toBe(cached);
  expect(reopened.result.current.error).toBe("Remote discovery failed");
});

it("never shows a late reply for another machine", async () => {
  const local = Promise.withResolvers<LaunchOptions>();
  api.options.mockImplementation((machine: string) => machine === "local" ? local.promise : Promise.resolve(options(machine)));
  const view = renderHook(({ machine }) => useHarnessOptions(machine), { initialProps: { machine: "local" } });
  view.rerender({ machine: "remote" });
  await waitFor(() => expect(view.result.current.options?.machine).toBe("remote"));
  await act(async () => { local.resolve(options("local")); await local.promise; });
  expect(view.result.current.options?.machine).toBe("remote");
  view.rerender({ machine: "local" });
  expect(view.result.current.options?.machine).toBe("local");
  expect(view.result.current.loading).toBe(false);
});

it("treats data from before a reconnect as stale even when no composer was mounted, and ignores its late reply", async () => {
  api.options.mockResolvedValue(options("local"));
  const view = renderHook(() => useHarnessOptions("local"));
  await waitFor(() => expect(view.result.current.options).toBeDefined());
  const cached = view.result.current.options;
  view.unmount();
  // The store bumps connectEpoch when the daemon becomes reachable again.
  useDaemon.setState({ link: "offline" });
  useDaemon.setState({ link: "connected", connectEpoch: 1 });
  const old = Promise.withResolvers<LaunchOptions>();
  const fresh = Promise.withResolvers<LaunchOptions>();
  api.options.mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise);
  const reopened = renderHook(() => useHarnessOptions("local"));
  expect(reopened.result.current.options).toBe(cached);
  expect(api.options).toHaveBeenCalledTimes(2);
  act(() => { useDaemon.setState({ link: "offline" }); });
  expect(reopened.result.current.offline).toBe(true);
  act(() => { useDaemon.setState({ link: "connected", connectEpoch: 2 }); });
  expect(api.options).toHaveBeenCalledTimes(3);
  const updated = { ...options("local"), worktree: false };
  await act(async () => { fresh.resolve(updated); await fresh.promise; });
  await act(async () => { old.resolve(options("local")); await old.promise; });
  expect(reopened.result.current.options).toBe(updated);
});
