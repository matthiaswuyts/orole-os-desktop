import { expect, it } from "vitest";
import type { FeedItem, TurnEntry } from "@/lib/taskEvents";
import { mapTurns } from "./conversationMap";

const at = "2026-10-12T10:00:00Z";
const user = (key: string, text: string): TurnEntry => ({ type: "item", key, at, item: { type: "user", key, at, text } as FeedItem });
const reply = (key: string, text: string): FeedItem => ({ type: "assistant", key, at, text } as FeedItem);
const run = (key: string, steps: FeedItem[], answer: FeedItem[]): TurnEntry => ({ type: "run", key, at, status: "complete", durationMs: 1000, steps, attention: [], answer } as unknown as TurnEntry);

it("starts a turn at every user message and previews the first reply", () => {
  const { turns, turnOfEntry } = mapTurns([
    user("u1", "  Fix the\nlogin redirect  "),
    run("r1", [reply("s1", "thinking out loud")], [reply("a1", "Done.\n\nThe redirect now keeps the query.")]),
    user("u2", "ok"),
    { type: "item", key: "a2", at, item: reply("a2", "Ready when you are.") },
  ]);
  expect(turnOfEntry).toEqual([0, 0, 1, 1]);
  expect(turns.map((turn) => [turn.id, turn.title, turn.preview])).toEqual([
    ["u1", "Fix the login redirect", "Done. The redirect now keeps the query."],
    ["u2", "ok", "Ready when you are."],
  ]);
  expect(turns[0]!.length).toBeGreaterThan(turns[1]!.length);
});

it("keeps activity from before the first loaded user message as its own turn", () => {
  const { turns, turnOfEntry } = mapTurns([{ type: "item", key: "a0", at, item: reply("a0", "Earlier answer") }, user("u1", "next")]);
  expect(turnOfEntry).toEqual([0, 1]);
  expect(turns[0]).toMatchObject({ id: "a0", title: "Earlier in this conversation", preview: "Earlier answer" });
});

it("truncates long titles and has no turns for an empty conversation", () => {
  expect(mapTurns([user("u", "x".repeat(200))]).turns[0]!.title).toHaveLength(90);
  expect(mapTurns([])).toEqual({ turns: [], turnOfEntry: [] });
});
