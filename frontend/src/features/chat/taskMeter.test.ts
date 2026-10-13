import { describe, expect, it } from "vitest";
import type { Session } from "@/mygo";
import { formatTokens, taskElapsedSeconds, taskTokens } from "./taskMeter";

const session = (patch: Partial<Session>): Session => ({ status: "working", startedAt: "2026-10-12T10:00:00Z", updatedAt: "2026-10-12T10:00:30Z", ...patch }) as Session;
const at = (iso: string) => Date.parse(iso);

describe("taskElapsedSeconds", () => {
  it("counts up to now while the task works", () => {
    expect(taskElapsedSeconds(session({}), at("2026-10-12T10:01:05Z"))).toBe(65);
  });
  it("freezes at the end time once the task stopped, however late it is read", () => {
    expect(taskElapsedSeconds(session({ status: "done", endedAt: "2026-10-12T10:00:42Z" }), at("2026-10-12T11:00:00Z"))).toBe(42);
  });
  it("falls back to the last update when a stopped task has no end time", () => {
    expect(taskElapsedSeconds(session({ status: "failed" }), at("2026-10-12T11:00:00Z"))).toBe(30);
  });
  it("is unknown without a usable start time, and never negative", () => {
    expect(taskElapsedSeconds(session({ startedAt: "" }), Date.now())).toBeUndefined();
    expect(taskElapsedSeconds(session({ startedAt: "garbage" }), Date.now())).toBeUndefined();
    expect(taskElapsedSeconds(session({}), at("2026-10-12T09:59:00Z"))).toBe(0);
  });
});

describe("taskTokens", () => {
  it("adds input and output tokens", () => {
    expect(taskTokens(session({ usage: { source: "sdk", inputTokens: 1200, outputTokens: 300, at: "" } }))).toBe(1500);
  });
  it("uses whichever side was reported", () => {
    expect(taskTokens(session({ usage: { source: "sdk", outputTokens: 7, at: "" } }))).toBe(7);
  });
  it("is unknown when nothing was reported", () => {
    expect(taskTokens(session({}))).toBeUndefined();
    expect(taskTokens(session({ usage: { source: "sdk", contextTokens: 5000, at: "" } }))).toBeUndefined();
  });
});

describe("formatTokens", () => {
  it("is compact", () => {
    expect(formatTokens(842)).toBe("842");
    expect(formatTokens(12_400)).toBe("12.4K");
    expect(formatTokens(1_250)).toBe("1.3K");
    expect(formatTokens(1_300_000)).toBe("1.3M");
  });
});
