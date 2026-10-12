import { describe, expect, it } from "vitest";
import { buildActivityGrid, compareRange, type TaskFact } from "./activity";

const now = new Date(2026, 9, 10, 15, 0); // Sat 2026-10-10
const at = (month: number, day: number, hour = 12) => ({ at: new Date(2026, month, day, hour).toISOString(), title: `${month}-${day}-${hour}` });

describe("buildActivityGrid", () => {
  it("lays out weeks Monday-first ending today and buckets by local day", () => {
    const g = buildActivityGrid([
      { at: new Date(2026, 9, 10, 0, 5).toISOString(), title: "a" },
      { at: new Date(2026, 9, 10, 23, 55).toISOString(), title: "b" },
      { at: new Date(2026, 9, 5, 12).toISOString(), title: "c" },
    ], now);
    const last = g.weeks.at(-1)!;
    expect(last[5]).toMatchObject({ date: "2026-10-10", count: 2, level: 4, titles: ["a", "b"] });
    expect(last[6]).toBeNull();
    expect(last[0]).toMatchObject({ date: "2026-10-05", count: 1, level: 2 });
    expect(g.total).toBe(3);
  });

  it("drops items outside the trailing year or with bad times", () => {
    const g = buildActivityGrid([
      { at: new Date(2025, 9, 9, 12).toISOString(), title: "too old" },
      { at: new Date(2026, 9, 11, 12).toISOString(), title: "future" },
      { at: "garbage", title: "bad" },
      { at: new Date(2025, 9, 11, 12).toISOString(), title: "oldest day in window" },
    ], now);
    expect(g.total).toBe(1);
  });

  it("covers exactly 365 days", () => {
    const g = buildActivityGrid([], now);
    expect(g.weeks.flat().filter(Boolean)).toHaveLength(365);
    expect(g.weeks.every((w) => w.length === 7)).toBe(true);
  });

  it("has no highlights without tasks", () => {
    expect(buildActivityGrid([], now).highlights).toEqual({ current: { days: 0, since: null }, longest: { days: 0, range: null }, month: null, weekday: null });
  });

  it("keeps the current streak alive through a today without tasks, and finds the longest run and busiest month and weekday", () => {
    const { highlights } = buildActivityGrid([
      // Current streak: Oct 7-9 (today, Oct 10, has none yet).
      at(9, 7), at(9, 8), at(9, 9),
      // Longest: Aug 3-7, five days, in the busiest month.
      at(7, 3), at(7, 4), at(7, 5), at(7, 6), at(7, 7), at(7, 20),
      // Wednesdays: Aug 5, Oct 7, and two more on Sep 2.
      at(8, 2), at(8, 2, 14),
    ], now);
    expect(highlights.current.days).toBe(3);
    expect(highlights.current.since).toMatch(/Oct\w* 7|7 Oct/);
    expect(highlights.longest.days).toBe(5);
    expect(highlights.longest.range).toMatch(/Aug.*3.*–.*7|3 Aug.*–.*7 Aug/);
    expect(highlights.month).toMatchObject({ count: 6 });
    expect(highlights.month!.label).toMatch(/Aug.*2026/);
    expect(highlights.weekday).toEqual({ label: "Wednesdays", count: 4 });
  });

  it("ends the current streak when neither today nor yesterday had a task", () => {
    expect(buildActivityGrid([at(9, 6), at(9, 7), at(9, 8)], now).highlights.current).toEqual({ days: 0, since: null });
    expect(buildActivityGrid([at(9, 9), at(9, 10)], now).highlights.current.days).toBe(2);
  });
});

describe("compareRange", () => {
  const fact = (startedAt: Date, patch: Partial<TaskFact> = {}): TaskFact => ({ startedAt: startedAt.toISOString(), finishedAt: "", completed: false, pullRequest: false, lines: 0, ...patch });

  it("compares this week so far with the same stretch of last week", () => {
    const { current, previous } = compareRange([
      fact(new Date(2026, 9, 5, 9), { pullRequest: true, lines: 40 }), // Mon this week
      fact(new Date(2026, 9, 10, 14), { lines: 2 }), // today, before now
      fact(new Date(2026, 9, 4, 23)), // Sun last week: neither window
      fact(new Date(2026, 8, 28, 9)), // Mon last week
      fact(new Date(2026, 9, 3, 16)), // Sat last week, after the same time: outside
    ], "week", now);
    expect(current).toEqual({ started: 2, completed: 0, pullRequests: 1, lines: 42 });
    expect(previous).toEqual({ started: 1, completed: 0, pullRequests: 0, lines: 0 });
  });

  it("counts completions by when they finished, not when they started", () => {
    const { current, previous } = compareRange([
      fact(new Date(2026, 8, 20), { completed: true, finishedAt: new Date(2026, 9, 6).toISOString() }),
      fact(new Date(2026, 9, 6), { completed: true, finishedAt: new Date(2026, 9, 2).toISOString() }),
    ], "week", now);
    expect(current.completed).toBe(1);
    expect(previous.completed).toBe(1);
  });

  it("clamps the previous month to its own length", () => {
    const end = new Date(2026, 2, 31, 12); // Mar 31
    const { previous } = compareRange([fact(new Date(2026, 1, 28, 9)), fact(new Date(2026, 2, 1, 9))], "month", end);
    expect(previous.started).toBe(1);
    const year = compareRange([fact(new Date(2025, 5, 1)), fact(new Date(2025, 11, 1))], "year", now);
    expect(year.previous.started).toBe(1);
  });
});
