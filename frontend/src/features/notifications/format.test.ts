import { describe, expect, it } from "vitest";
import { dayGroup, shortTime } from "./format";

const at = (y: number, m: number, d: number, h = 12, min = 0) => new Date(y, m - 1, d, h, min).getTime();
const unix = (ms: number) => Math.floor(ms / 1000);

describe("notification time", () => {
  const now = at(2026, 10, 14, 0, 30); // just after midnight

  it("groups by local calendar day, not by 24-hour spans", () => {
    expect(dayGroup(unix(at(2026, 10, 14, 0, 5)), now)).toBe("Today");
    expect(dayGroup(unix(at(2026, 10, 13, 23, 50)), now)).toBe("Yesterday");
    expect(dayGroup(unix(at(2026, 10, 8, 9)), now)).toBe("This week");
    expect(dayGroup(unix(at(2026, 10, 7, 9)), now)).toBe("Earlier");
  });

  it("labels rows compactly", () => {
    const noon = at(2026, 10, 14, 12);
    expect(shortTime(unix(noon - 20_000), noon)).toBe("now");
    expect(shortTime(unix(noon - 8 * 60_000), noon)).toBe("8m");
    expect(shortTime(unix(noon - 2 * 3_600_000), noon)).toBe("2h");
    expect(shortTime(unix(at(2026, 10, 13, 23, 50)), now)).toBe("Yesterday");
    expect(shortTime(unix(at(2025, 10, 5)), now)).toMatch(/2025/);
  });
});
