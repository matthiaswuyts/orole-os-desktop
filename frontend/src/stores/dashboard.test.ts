import { describe, expect, it } from "vitest";
import { DEFAULT_WIDGETS, normalizeWidgets } from "./dashboard";

describe("normalizeWidgets", () => {
  it("orders a free-form grid layout by row then column and maps widths and heights to sizes", () => {
    expect(normalizeWidgets([
      { id: "notes", type: "notes", x: 0, y: 5, w: 5, h: 6, config: { text: "kept" } },
      { id: "setup", type: "setup", x: 8, y: 0, w: 4, h: 5 },
      { id: "daemon", type: "daemon-health", x: 0, y: 0, w: 3, h: 5 },
      { id: "sys", type: "activity", x: 6, y: 5, w: 7, h: 9 },
      { id: "wide", type: "plugin.x/y", x: 0, y: 14, w: 11, h: 4 },
    ])).toEqual([
      { id: "daemon", type: "daemon-health", size: "s" },
      { id: "setup", type: "setup", size: "s" },
      { id: "notes", type: "notes", size: "m", config: { text: "kept" } },
      { id: "sys", type: "activity", size: "l", tall: true },
      { id: "wide", type: "plugin.x/y", size: "full" },
    ]);
  });

  it("turns the former heat graph into a full-width Overview", () => {
    expect(normalizeWidgets([{ id: "w-heat", type: "activity-heatmap", x: 0, y: 11, w: 8, h: 12 }])).toEqual([{ id: "w-heat", type: "overview", size: "full" }]);
  });

  it("keeps a current layout's order, sizes and unknown types, and drops malformed or duplicate entries", () => {
    expect(normalizeWidgets([
      { id: "b", type: "clock", size: "l", tall: true },
      { id: "a", type: "gone.plugin", size: "s" },
      { id: "c", type: "notes", size: "huge" },
      { id: "b", type: "notes", size: "s" },
      { type: "notes" },
      null,
      "x",
      { id: "d", type: "notes", size: "constructor" },
    ])).toEqual([
      { id: "b", type: "clock", size: "l", tall: true },
      { id: "a", type: "gone.plugin", size: "s" },
      { id: "c", type: "notes", size: "m" },
      { id: "d", type: "notes", size: "m" },
    ]);
  });

  it("uses the defaults only when nothing was saved, so an emptied dashboard stays empty", () => {
    expect(normalizeWidgets(undefined)).toBe(DEFAULT_WIDGETS);
    expect(normalizeWidgets([])).toEqual([]);
  });
});
