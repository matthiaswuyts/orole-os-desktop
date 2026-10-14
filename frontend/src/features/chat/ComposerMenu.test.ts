import { describe, expect, it } from "vitest";
import { matchItems, menuQuery } from "./ComposerMenu";

describe("menuQuery", () => {
  it("opens on @ or / at the start or after whitespace, with what follows up to the caret", () => {
    expect(menuQuery("@", 1)).toEqual({ trigger: "@", start: 0, query: "" });
    expect(menuQuery("ask @Res", 8)).toEqual({ trigger: "@", start: 4, query: "Res" });
    expect(menuQuery("line one\n/pd", 12)).toEqual({ trigger: "/", start: 9, query: "pd" });
  });

  it("keeps a multi-word query, so names with spaces stay findable", () => {
    expect(menuQuery("@Research B", 11)?.query).toBe("Research B");
  });

  it("ignores paths, mail addresses, a trigger on an earlier line, and a space right after the trigger", () => {
    expect(menuQuery("see src/app", 11)).toBeNull();
    expect(menuQuery("mail me@x.io", 12)).toBeNull();
    expect(menuQuery("@bot\nnext", 9)).toBeNull();
    expect(menuQuery("a @ b", 5)).toBeNull();
  });

  it("only looks at text before the caret", () => {
    expect(menuQuery("@bot then", 2)).toEqual({ trigger: "@", start: 0, query: "b" });
  });
});

describe("matchItems", () => {
  const items = [{ label: "Research Bot" }, { label: "Coach" }, { label: "Deep research" }, { label: "pdf-reader" }];

  it("ranks a label that starts with the query, then a word that does, then one that contains it", () => {
    expect(matchItems(items, "re").map((i) => i.label)).toEqual(["Research Bot", "Deep research", "pdf-reader"]);
    expect(matchItems([{ label: "Thread" }, { label: "Read" }, { label: "Old reader" }], "rea").map((i) => i.label)).toEqual(["Read", "Old reader", "Thread"]);
  });

  it("ignores case and lists everything for an empty query", () => {
    expect(matchItems(items, "COA").map((i) => i.label)).toEqual(["Coach"]);
    expect(matchItems(items, "")).toHaveLength(4);
  });
});
