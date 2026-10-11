import { describe, expect, it } from "vitest";
import { splitQuote, withQuote } from "./quote";

describe("quoted messages", () => {
  it("round-trips a multi-line passage and the request", () => {
    const text = withQuote("First line.\n\nSecond line.", "Shorten this selected text.");
    expect(text).toBe("> First line.\n>\n> Second line.\n\nShorten this selected text.");
    expect(splitQuote(text)).toEqual({ quote: "First line.\n\nSecond line.", rest: "Shorten this selected text." });
  });

  it("cuts a very long passage", () => {
    const { quote } = splitQuote(withQuote("x".repeat(5000), "Explain."));
    expect(quote.length).toBeLessThan(1600);
    expect(quote.endsWith("…")).toBe(true);
  });

  it("leaves a message without a quote alone, including a > in the middle", () => {
    expect(splitQuote("plain")).toEqual({ quote: "", rest: "plain" });
    expect(splitQuote("a\n> not a quote")).toEqual({ quote: "", rest: "a\n> not a quote" });
  });
});
