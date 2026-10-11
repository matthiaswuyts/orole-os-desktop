import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { FOCUS_MIN_INTERVAL_MS, onFocusRefresh } from "@/lib/focus";

const focus = () => window.dispatchEvent(new Event("focus"));
let stops: (() => void)[] = [];
const subscribe = (listener: () => void) => { const stop = onFocusRefresh(listener); stops.push(stop); return stop; };

beforeEach(() => { vi.useFakeTimers({ toFake: ["Date"] }); vi.setSystemTime(1_000_000); });
afterEach(() => { stops.forEach((stop) => stop()); stops = []; vi.useRealTimers(); });

describe("shared focus signal", () => {
  it("fans one throttled focus out to every subscriber", () => {
    const a = vi.fn();
    const b = vi.fn();
    subscribe(a); subscribe(b);
    for (let i = 0; i < 20; i++) focus();
    expect([a.mock.calls.length, b.mock.calls.length]).toEqual([0, 0]);
    vi.setSystemTime(Date.now() + FOCUS_MIN_INTERVAL_MS);
    for (let i = 0; i < 20; i++) focus();
    expect([a.mock.calls.length, b.mock.calls.length]).toEqual([1, 1]);
    vi.setSystemTime(Date.now() + FOCUS_MIN_INTERVAL_MS - 1);
    focus();
    expect(a).toHaveBeenCalledTimes(1);
    vi.setSystemTime(Date.now() + 1);
    focus();
    expect(a).toHaveBeenCalledTimes(2);
  });

  it("registers a single window listener however many stores subscribe, and removes it with the last", () => {
    const add = vi.spyOn(window, "addEventListener");
    const remove = vi.spyOn(window, "removeEventListener");
    const stopA = subscribe(() => {});
    const stopB = subscribe(() => {});
    expect(add.mock.calls.filter(([type]) => type === "focus")).toHaveLength(1);
    stopA();
    expect(remove.mock.calls.filter(([type]) => type === "focus")).toHaveLength(0);
    stopB();
    expect(remove.mock.calls.filter(([type]) => type === "focus")).toHaveLength(1);
    add.mockRestore(); remove.mockRestore();
  });

  it("does not deliver to an unsubscribed listener and survives a throwing one", () => {
    const quiet = vi.fn();
    const after = vi.fn();
    vi.spyOn(console, "error").mockImplementation(() => {});
    subscribe(() => { throw new Error("boom"); });
    subscribe(after);
    subscribe(quiet)();
    vi.setSystemTime(Date.now() + FOCUS_MIN_INTERVAL_MS);
    focus();
    expect(after).toHaveBeenCalledTimes(1);
    expect(quiet).not.toHaveBeenCalled();
  });
});
