import { describe, expect, it } from "vitest";
import { effortChoices, effortLabel, modelLabel, providerLabel, supportedEffort } from "./model-options";

describe("model labels", () => {
  it("uses the actual resolved version rather than default annotations or raw ids", () => {
    expect(modelLabel({ id: "default", name: "Default (recommended) (claude-opus-5-5)" })).toBe("Opus 5.5");
    expect(modelLabel({ id: "haiku", name: "Haiku (claude-haiku-4-5-20251001)" })).toBe("Haiku 4.5");
    expect(modelLabel({ id: "opus", name: "Opus 5.5" })).toBe("Opus 5.5");
  });

  it("keeps reported variants distinct without inventing model versions", () => {
    expect(modelLabel({ id: "opus[1m]", name: "Opus (1M context) (claude-opus-5-5[1m])" })).toBe("Opus 5.5 (1M)");
    expect(modelLabel({ id: "unknown-selector", name: "Authoritative SDK label" })).toBe("Authoritative SDK label");
    expect(modelLabel({ id: "default", name: "Default (recommended)" })).toBe("Provider default");
    expect(modelLabel(undefined)).toBe("Provider default");
    expect(modelLabel({ id: "gpt-5.4", name: "gpt-5.4" })).toBe("GPT-5.4");
    expect(providerLabel({ id: "omp", name: "Oh My Pi" })).toBe("Pi");
  });
});

describe("reasoning effort boundaries", () => {
  it("only exposes reported levels plus the unset Auto stop", () => {
    expect(effortChoices(["high", "low", "high", "", "xhigh"])).toEqual(["", "low", "high", "xhigh"]);
    expect(effortChoices([])).toEqual([""]);
    expect(effortChoices(["high"])).toEqual(["", "high"]);
    expect(effortLabel("")).toBe("Auto");
    expect(effortLabel("xhigh")).toBe("X-High");
  });

  it("preserves supported selections and clears unsupported model overrides", () => {
    expect(supportedEffort(["low", "high"], "high")).toBe("high");
    expect(supportedEffort(["low"], "high")).toBe("");
    expect(supportedEffort([], "high")).toBe("");
    expect(supportedEffort(["high"], "")).toBe("");
  });
});
