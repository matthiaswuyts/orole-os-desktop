import { describe, expect, it } from "vitest";
import { fillRows } from "./frame";

describe("fillRows", () => {
  it("keeps rows that already fill the grid", () => {
    expect(fillRows([{ span: 12 }, { span: 8 }, { span: 4 }, { span: 4 }, { span: 4 }, { span: 4 }])).toEqual([12, 8, 4, 4, 4, 4]);
  });

  it("widens the last widget of a short row to the right edge, including the final row", () => {
    // Medium + Small (10 of 12), then Large alone (Small does not fit beside it on its row).
    expect(fillRows([{ span: 6 }, { span: 4 }, { span: 8 }, { span: 6 }])).toEqual([6, 6, 12, 12]);
  });

  it("widens only into columns left free by a tall widget on both of its rows", () => {
    // Tall Small on rows 1-2; two Smalls fill row 1; row 2 has one Small that widens to the edge.
    expect(fillRows([{ span: 4, tall: true }, { span: 4 }, { span: 4 }, { span: 4 }])).toEqual([4, 4, 4, 8]);
    // A tall Medium after a Small widens over both of its rows; the Small below the first one cannot (the tall one is beside it).
    expect(fillRows([{ span: 4 }, { span: 6, tall: true }, { span: 4 }])).toEqual([4, 8, 4]);
  });

  it("places a widget that does not fit before a tall one on the next free row", () => {
    // Row 2 has 8 free columns beside the tall Small, so the Large goes there.
    expect(fillRows([{ span: 4, tall: true }, { span: 8 }, { span: 8 }, { span: 12 }])).toEqual([4, 8, 8, 12]);
  });
});
