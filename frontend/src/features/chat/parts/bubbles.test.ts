import { describe, expect, it } from "vitest";
import { splitBubbles } from "./bubbles";

describe("splitBubbles", () => {
  it("makes one bubble per paragraph", () => {
    expect(splitBubbles("On it.\n\nI found two issues.\n\n\nFixing the first now.")).toEqual(["On it.", "I found two issues.", "Fixing the first now."]);
  });

  it("keeps a fenced code block whole, blank lines included", () => {
    const text = "Run this:\n\n```sh\nbun install\n\nbun run dev\n```\n\nThen open the app.";
    expect(splitBubbles(text)).toEqual(["Run this:\n\n```sh\nbun install\n\nbun run dev\n```", "Then open the app."]);
  });

  it("keeps a lead-in line with the list it introduces", () => {
    expect(splitBubbles("Three things broke:\n\n- auth\n- billing\n- search\n\nStarting with auth.")).toEqual(["Three things broke:\n\n- auth\n- billing\n- search", "Starting with auth."]);
  });

  it("keeps a list split by blank lines together", () => {
    expect(splitBubbles("1. one\n\n2. two\n\nDone.")).toEqual(["1. one\n\n2. two", "Done."]);
  });

  it("leaves earlier bubbles untouched while the last one streams", () => {
    const early = splitBubbles("First point.\n\nSecon");
    const later = splitBubbles("First point.\n\nSecond point is longer.");
    expect(early[0]).toBe(later[0]);
    expect(later).toHaveLength(2);
  });

  it("drops empty input and keeps a table in one piece", () => {
    expect(splitBubbles(" \n\n ")).toEqual([]);
    expect(splitBubbles("| a | b |\n|---|---|\n| 1 | 2 |")).toHaveLength(1);
  });
});
