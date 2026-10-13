import { describe, expect, it } from "vitest";
import type { Session } from "@/mygo";
import { activityOf, busyBotIds } from "./botActivity";

const session = (id: string, status: string, owner?: string, review = ""): Session => ({ id, status, owner, review }) as unknown as Session;

describe("busyBotIds", () => {
  it("is empty when there are no tasks", () => expect(busyBotIds({})).toEqual([]));

  it("lists each bot with a working task once, sorted", () => {
    expect(busyBotIds({
      a: session("a", "working", "bot:zed"), b: session("b", "working", "bot:ann"), c: session("c", "working", "bot:ann"),
    })).toEqual(["ann", "zed"]);
  });

  it("ignores tasks that are not in progress or not owned by a bot", () => {
    expect(busyBotIds({
      a: session("a", "done", "bot:ann"), b: session("b", "failed", "bot:ann"), c: session("c", "stopped", "bot:ann"),
      d: session("d", "lost", "bot:ann"), e: session("e", "idle", "bot:ann"), f: session("f", "needs-you", "bot:ann"),
      g: session("g", "working", ""), h: session("h", "working"), i: session("i", "working", "agent:x"),
      j: session("j", "working", "bot:ann", "discarded"),
    })).toEqual([]);
  });
});

describe("activityOf", () => {
  it("is idle with neither a reply nor a task", () => expect(activityOf(false, false)).toBe("idle"));
  it("is working with only a task", () => expect(activityOf(false, true)).toBe("working"));
  it("is responding while replying, even with a task", () => {
    expect(activityOf(true, false)).toBe("responding");
    expect(activityOf(true, true)).toBe("responding");
  });
});
