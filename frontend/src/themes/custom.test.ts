import { describe, expect, it } from "vitest";
import { validateCustomTokens } from "./custom";

describe("validateCustomTokens", () => {
  it("accepts whitelisted tokens with colour values", () => {
    expect(validateCustomTokens({ primary: "#8fb0ff", destructive: "oklch(0.7 0.18 25)" })).toMatchObject({ ok: true });
  });
  it("rejects unknown tokens and values that could inject CSS", () => {
    expect(validateCustomTokens({ "background-image": "#fff" })).toMatchObject({ ok: false });
    expect(validateCustomTokens({ primary: "red; background: url(http://evil)" })).toMatchObject({ ok: false });
    expect(validateCustomTokens({ primary: "url(http://evil)" })).toMatchObject({ ok: false });
    expect(validateCustomTokens([1, 2])).toMatchObject({ ok: false });
  });
});
