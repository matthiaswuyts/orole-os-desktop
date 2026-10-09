import { canvas, names } from "ascii.rest";
import { expect, it } from "vitest";
import { MARK_PIECES, pickMark } from "./AsciiMark";

it("offers only installed text pieces, which draw into a <pre>", () => {
  for (const piece of MARK_PIECES) {
    expect(names, piece).toContain(piece);
    expect(canvas.has(piece), `${piece} needs a canvas`).toBe(false);
  }
  expect(new Set(MARK_PIECES).size).toBe(13);
});

it("picks any piece from the list and never one outside it", () => {
  expect(pickMark(() => 0)).toBe("donut");
  expect(pickMark(() => 0.999999)).toBe("windmill");
  const seen = new Set(MARK_PIECES.map((_, index) => pickMark(() => index / MARK_PIECES.length)));
  expect(seen.size).toBe(MARK_PIECES.length);
});
