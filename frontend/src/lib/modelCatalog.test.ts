import { describe, expect, it } from "vitest";
import { CURATED, matchCurated, normalizeModelId } from "./modelCatalog";
import { companyOf, isChatModel } from "./models";

describe("matchCurated", () => {
  it("recognises a model through vendor prefixes, dots and dated snapshots", () => {
    expect(matchCurated("anthropic/claude-opus-5.5")?.name).toBe("Claude Opus 5.5");
    expect(matchCurated("models/gemini-4-argon")?.name).toBe("Gemini 4 Argon");
    expect(matchCurated("claude-sonnet-5-5-20261001")?.name).toBe("Claude Sonnet 5.5");
  });

  it("never mistakes a different product for the curated base model", () => {
    expect(matchCurated("gemini-3-8-flash-lite")).toBeUndefined();
    expect(matchCurated("gpt-6-astra-pro")).toBeUndefined();
    expect(matchCurated("claude-haiku-5-5-mini")).toBeUndefined();
    expect(matchCurated("gpt-6-1-sol")?.name).toBe("GPT-6.1 Sol");
    expect(matchCurated("gpt-6-1")).toBeUndefined();
    expect(matchCurated("claude-opus-5-50")).toBeUndefined();
  });

  it("lists every curated id once, already normalized", () => {
    const ids = CURATED.map((m) => m.id);
    expect(new Set(ids).size).toBe(ids.length);
    for (const id of ids) expect(normalizeModelId(id)).toBe(id);
  });
});

describe("chat model filter and company", () => {
  it("drops embedding, image and audio models but keeps chat models", () => {
    for (const id of ["text-embedding-3-large", "dall-e-3", "gpt-image-1", "whisper-1", "gpt-4o-mini-tts", "omni-moderation-latest", "rerank-v3"]) {
      expect(isChatModel({ id, name: id })).toBe(false);
    }
    for (const id of ["claude-opus-5-5", "gpt-6-astra", "gemini-4-argon", "deepseek-v4-1-flash"]) expect(isChatModel({ id, name: id })).toBe(true);
  });

  it("names the company from an aggregator prefix or the model family", () => {
    expect(companyOf({ id: "x-ai/grok-4.7", name: "" })).toBe("xAI");
    expect(companyOf({ id: "models/gemini-4-argon", name: "" })).toBe("Google");
    expect(companyOf({ id: "mimo-v2-6-pro", name: "" })).toBe("Xiaomi");
    expect(companyOf({ id: "my-finetune", name: "" })).toBe("Other");
  });
});
