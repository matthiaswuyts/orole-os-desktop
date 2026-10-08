import { describe, expect, it } from "vitest";
import { fuzzyScore } from "./commands";

describe("fuzzyScore", () => {
  it("ranks prefix > substring > subsequence and rejects non-matches", () => {
    const pre = fuzzyScore("go", "Go to Mail"), sub = fuzzyScore("mail", "Go to Mail"), seq = fuzzyScore("gtm", "Go to Mail");
    expect(pre).toBeGreaterThan(sub);
    expect(sub).toBeGreaterThan(seq);
    expect(seq).toBeGreaterThan(0);
    expect(fuzzyScore("zzz", "Go to Mail")).toBe(0);
  });
  it("matches everything for an empty query", () => expect(fuzzyScore("", "x")).toBeGreaterThan(0));
});
