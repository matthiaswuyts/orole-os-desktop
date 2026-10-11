import { describe, expect, it } from "vitest";
import { buildActivityGrid } from "./activity";

const now = new Date(2026, 9, 10, 15, 0); // Sat 2026-10-10

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
});
