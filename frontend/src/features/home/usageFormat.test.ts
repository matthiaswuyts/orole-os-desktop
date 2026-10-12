import { describe, expect, it } from "vitest";
import type { DailyUsage } from "@/mygo";
import { compactNumber, costLabel, sparkSeries, usd } from "./usageFormat";

describe("compactNumber", () => {
  it("keeps counts under a thousand whole and adds a unit with two decimals from there", () => {
    expect(compactNumber(0)).toBe("0");
    expect(compactNumber(999)).toBe("999");
    expect(compactNumber(1_000)).toBe("1.00K");
    expect(compactNumber(772_800)).toBe("772.80K");
    expect(compactNumber(937_700_000)).toBe("937.70M");
    expect(compactNumber(1.88e9)).toBe("1.88B");
    expect(compactNumber(2.5e12)).toBe("2.50T");
  });

  it("moves to the next unit when rounding reaches a thousand of the current one", () => {
    expect(compactNumber(999.6)).toBe("1.00K");
    expect(compactNumber(999_994)).toBe("999.99K");
    expect(compactNumber(999_995)).toBe("1.00M");
    expect(compactNumber(999_995_000)).toBe("1.00B");
  });

  it("reads invalid or negative counts as zero", () => {
    expect(compactNumber(Number.NaN)).toBe("0");
    expect(compactNumber(-5)).toBe("0");
  });
});

describe("usd", () => {
  it("rounds to cents and marks spend under half a cent", () => {
    expect(usd(0)).toBe("$0.00");
    expect(usd(0.004)).toBe("<$0.01");
    expect(usd(0.005)).toBe("$0.01");
    expect(usd(586.174)).toBe("$586.17");
    expect(usd(586.175)).toBe("$586.18");
    expect(usd(12_345.6)).toBe("$12,345.60");
  });
});

describe("costLabel", () => {
  it("prefixes ≥ only when some models are unpriced", () => {
    expect(costLabel({ costUsd: 586.17, unpriced: [] })).toBe("$586.17");
    expect(costLabel({ costUsd: 586.17, unpriced: ["armor/opencode"] })).toBe("≥ $586.17");
    expect(costLabel({ costUsd: 0.001, unpriced: [] })).toBe("<$0.01");
    expect(costLabel({ costUsd: 0.001, unpriced: ["x/y"] })).toBe("≥ $0.00");
  });
});

describe("sparkSeries", () => {
  const daily: DailyUsage[] = Array.from({ length: 365 }, (_, i) => ({ day: `d${i}`, sessions: 0, tokens: i + 1, costUsd: 0 }));

  it("takes the last seven days", () => {
    expect(sparkSeries(daily, false, (d) => d.tokens)).toEqual([359, 360, 361, 362, 363, 364, 365]);
  });

  it("sums weeks back from today for the year, the oldest partial week first", () => {
    const weeks = sparkSeries(daily, true, (d) => d.tokens);
    expect(weeks).toHaveLength(53);
    expect(weeks[0]).toBe(1); // day 1 alone: 365 = 52 × 7 + 1
    expect(weeks.at(-1)).toBe(359 + 360 + 361 + 362 + 363 + 364 + 365);
    expect(weeks.reduce((a, b) => a + b, 0)).toBe((365 * 366) / 2);
  });
});
