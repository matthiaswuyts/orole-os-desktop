import { beforeEach, describe, expect, it } from "vitest";
import { useChatDrafts } from "@/stores/chatDrafts";

beforeEach(() => useChatDrafts.setState({ drafts: {} }));

describe("chat drafts", () => {
  it("keeps drafts per bot", () => {
    useChatDrafts.getState().setDraft("a", { text: "one" });
    useChatDrafts.getState().setDraft("b", { text: "two", research: true });
    expect(useChatDrafts.getState().drafts).toEqual({ a: { text: "one", research: false }, b: { text: "two", research: true } });
  });

  it("does not notify when a patch changes nothing", () => {
    useChatDrafts.getState().setDraft("a", { text: "one" });
    const before = useChatDrafts.getState().drafts;
    useChatDrafts.getState().setDraft("a", { text: "one" });
    expect(useChatDrafts.getState().drafts).toBe(before);
  });

  it("drops one bot's draft", () => {
    useChatDrafts.getState().setDraft("a", { text: "one" });
    useChatDrafts.getState().setDraft("b", { text: "two" });
    useChatDrafts.getState().dropDraft("a");
    expect(Object.keys(useChatDrafts.getState().drafts)).toEqual(["b"]);
  });
});
