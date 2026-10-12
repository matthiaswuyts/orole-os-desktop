import { describe, expect, it } from "vitest";
import { effortChoices, effortLabel, modelContext, modelLabel, modelTitle, providerLabel, supportedEffort } from "./model-options";

describe("model labels", () => {
  it("names Claude models in full with major.minor from the resolved model, never from annotations or dates", () => {
    expect(modelLabel({ id: "default", name: "Default (recommended) (claude-opus-5-5)" })).toBe("Claude Opus 5.5");
    expect(modelLabel({ id: "haiku", name: "Haiku (claude-haiku-4-5-20251001)" })).toBe("Claude Haiku 4.5");
    expect(modelLabel({ id: "sonnet", name: "Sonnet (claude-sonnet-5)" })).toBe("Claude Sonnet 5.0");
    expect(modelLabel({ id: "opus", name: "Opus 5.5" })).toBe("Claude Opus 5.5");
  });

  it("lists the context window, so the 1M variant is told apart from the standard one", () => {
    const long = { id: "opus[1m]", name: "Opus (1M context) (claude-opus-5[1m])" };
    expect(modelLabel(long)).toBe("Claude Opus 5.0");
    expect(modelContext(long)).toBe("1M");
    expect(modelContext({ id: "claude-fable-5-1[1m]", name: "Fable (claude-fable-5-1)" })).toBe("1M");
    expect(modelLabel({ id: "claude-fable-5-1[1m]", name: "Fable (claude-fable-5-1)" })).toBe("Claude Fable 5.1");
    // No window is claimed where Claude Code reports none.
    expect(modelTitle({ id: "sonnet", name: "Sonnet (claude-sonnet-5)" })).toBe("Claude Sonnet 5.0");
    expect(modelTitle(long)).toBe("Claude Opus 5.0 · 1M context");
    expect(modelContext({ id: "gpt-5.4", name: "gpt-5.4" })).toBe("");
    expect(modelTitle({ id: "gpt-5.4", name: "gpt-5.4" })).toBe("GPT-5.4");
  });

  it("does not invent versions for labels it cannot identify", () => {
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
