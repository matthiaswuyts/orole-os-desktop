import { describe, expect, it } from "vitest";
import { LIMITS, TOOL_IDS, copyName, emptyToolSettings, fingerprint, formFromBot, formReducer, initialForm, parseBotExport, parseDomain, tabOfProblem, toInput, validate, type FormState } from "./botForm";
import { bot, config } from "./testing";

const valid = (over: Partial<FormState> = {}): FormState => ({ ...initialForm(), name: "Planner", ...over });

describe("validate", () => {
  it("accepts a minimal bot", () => {
    expect(validate(valid())).toEqual({});
  });

  it("requires a name and limits it to 80 characters", () => {
    expect(validate(valid({ name: "   " })).name).toMatch(/name/i);
    expect(validate(valid({ name: "x".repeat(LIMITS.name) })).name).toBeUndefined();
    expect(validate(valid({ name: "x".repeat(LIMITS.name + 1) })).name).toMatch(/80/);
  });

  it("limits the avatar to two graphemes", () => {
    expect(validate(valid({ avatar: "🧭" })).avatar).toBeUndefined();
    expect(validate(valid({ avatar: "AB" })).avatar).toBeUndefined();
    expect(validate(valid({ avatar: "ABC" })).avatar).toBeDefined();
  });

  it("limits description to 500 and instructions to 32 KiB characters", () => {
    expect(validate(valid({ description: "d".repeat(501) })).description).toMatch(/500/);
    expect(validate(valid({ instructions: "i".repeat(32 * 1024) })).instructions).toBeUndefined();
    expect(validate(valid({ instructions: "i".repeat(32 * 1024 + 1) })).instructions).toMatch(/32,768/);
  });

  it("checks the turn limits (steps 1-30, tool calls 1-60, empty = default)", () => {
    expect(validate(valid({ maxSteps: "", maxToolCalls: "" }))).toEqual({});
    expect(validate(valid({ maxSteps: "30", maxToolCalls: "60" }))).toEqual({});
    expect(validate(valid({ maxSteps: "0" })).maxSteps).toBeDefined();
    expect(validate(valid({ maxSteps: "31" })).maxSteps).toBeDefined();
    expect(validate(valid({ maxToolCalls: "61" })).maxToolCalls).toBeDefined();
    expect(validate(valid({ maxToolCalls: "2.5" })).maxToolCalls).toBeDefined();
  });

  it("checks tool parameters and domain list sizes", () => {
    const tool = (patch: Partial<ReturnType<typeof emptyToolSettings>>) => ({ [TOOL_IDS.webSearch]: { ...emptyToolSettings(), ...patch } });
    expect(validate(valid({ tools: tool({ maxResults: "5" }) }))).toEqual({});
    expect(validate(valid({ tools: tool({ maxResults: "11" }) }))[`tool:${TOOL_IDS.webSearch}:maxResults`]).toBeDefined();
    const many = Array.from({ length: 21 }, (_, i) => `site${i}.example.com`);
    expect(validate(valid({ tools: tool({ allowed: many }) }))[`tool:${TOOL_IDS.webSearch}:allowed`]).toMatch(/20/);
    expect(validate(valid({ tools: tool({ blocked: many }) }))[`tool:${TOOL_IDS.webSearch}:blocked`]).toMatch(/20/);
    expect(validate(valid({ tools: { [TOOL_IDS.webFetch]: { ...emptyToolSettings(), maxContentTokens: "100001" } } }))[`tool:${TOOL_IDS.webFetch}:maxContentTokens`]).toBeDefined();
  });

  it("maps problems to the tab that holds the field", () => {
    expect(tabOfProblem("name")).toBe("persona");
    expect(tabOfProblem("maxSteps")).toBe("model");
    expect(tabOfProblem("tool:web_search:allowed")).toBe("tools");
    expect(tabOfProblem("skills")).toBe("tools");
  });
});

describe("parseDomain", () => {
  it("normalises valid host names", () => {
    expect(parseDomain(" Example.COM. ")).toEqual({ ok: true, domain: "example.com" });
    expect(parseDomain("docs.example.co.uk")).toMatchObject({ ok: true });
  });

  it.each(["", "localhost", "https://example.com", "example.com/path", "example.com:8080", "10.0.0.1", "exa mple.com", "-bad.example.com", "a..b.com", "user@example.com"])("rejects %j", (input) => {
    expect(parseDomain(input).ok).toBe(false);
  });
});

describe("toInput", () => {
  it("creates with an empty id and sends no params for untouched tools", () => {
    const form = valid({ model: "openrouter/gpt-x", tools: { [TOOL_IDS.datetime]: emptyToolSettings() } });
    const input = toInput(form, "");
    expect(input).toMatchObject({ id: "", name: "Planner", provider: "openrouter", model: "gpt-x" });
    expect(input.config.tools).toEqual([{ id: "datetime" }]);
    expect(input.config.limits).toEqual({ maxSteps: 0, maxToolCalls: 0 });
  });

  it("keeps slashes of the model id and maps an empty model to the defaults", () => {
    expect(toInput(valid({ model: "openrouter/anthropic/claude" }), "b1")).toMatchObject({ id: "b1", provider: "openrouter", model: "anthropic/claude" });
    expect(toInput(valid({ model: "" }), "b1")).toMatchObject({ provider: "", model: "" });
  });

  it("writes tool params under the daemon's names and carries unknown params through", () => {
    const form = valid({
      tools: {
        [TOOL_IDS.webSearch]: { ...emptyToolSettings(), maxResults: "4", allowed: ["a.com"], blocked: ["b.com"], rest: { region: "eu" } },
        [TOOL_IDS.webFetch]: { ...emptyToolSettings(), maxContentTokens: "2000" },
      },
    });
    expect(toInput(form, "b1").config.tools).toEqual([
      { id: "web_search", params: { region: "eu", max_results: 4, allowed_domains: ["a.com"], blocked_domains: ["b.com"] } },
      { id: "web_fetch", params: { max_content_tokens: 2000 } },
    ]);
  });

  it("round-trips a saved bot", () => {
    const saved = bot("b1", {
      name: "Researcher", provider: "openrouter", model: "gpt-x", reasoning: "low",
      config: config({
        tools: [{ id: "web_search", params: { max_results: 3, allowed_domains: ["a.com"] } }], mcpTools: ["srv/tool"], skills: ["s"], delegateTo: ["b2"],
        memory: [{ scope: "user", read: true, write: false }], limits: { maxSteps: 5, maxToolCalls: 12 }, memoryApproval: true,
      }),
    });
    const input = toInput(formFromBot(saved), "b1");
    expect(input).toMatchObject({ id: "b1", reasoning: "low", config: { ...saved.config } });
  });
});

describe("formReducer access", () => {
  it("grants write together with read, and clearing read clears write", () => {
    let form = formReducer(valid(), { type: "access", scope: "user", read: false, write: true });
    expect(form.memory).toEqual([{ scope: "user", read: true, write: true }]);
    form = formReducer(form, { type: "access", scope: "user", read: false, write: false });
    expect(form.memory).toEqual([]);
  });

  it("detects unsaved changes independent of toggle order", () => {
    const a = valid({ skills: ["x", "y"] });
    expect(fingerprint(a)).toBe(fingerprint(valid({ skills: ["y", "x"] })));
    expect(fingerprint(a)).not.toBe(fingerprint(valid({ skills: ["x"] })));
  });
});

describe("parseBotExport", () => {
  const doc = (over: Record<string, unknown> = {}) => JSON.stringify({ version: 1, bot: { name: "Imported", instructions: "Be brief", color: "red", config: { tools: [{ id: "datetime" }], memory: [{ scope: "user", read: true }] } }, ...over });

  it("accepts version 1 and fills defaults", () => {
    const parsed = parseBotExport(doc());
    expect(parsed).toMatchObject({ ok: true, value: { version: 1, bot: { id: "", name: "Imported", color: "red", instructions: "Be brief" } } });
    if (parsed.ok) {
      expect(parsed.value.bot.config.tools).toEqual([{ id: "datetime" }]);
      expect(parsed.value.bot.config.memory).toEqual([{ scope: "user", read: true, write: false }]);
      expect(parsed.value.bot.config.limits).toEqual({ maxSteps: 0, maxToolCalls: 0 });
    }
  });

  it("rejects other versions, missing names and malformed input", () => {
    expect(parseBotExport(doc({ version: 2 }))).toMatchObject({ ok: false, error: expect.stringContaining("version 2") });
    expect(parseBotExport(JSON.stringify({ bot: { name: "x" } }))).toMatchObject({ ok: false, error: expect.stringContaining("no version") });
    expect(parseBotExport(JSON.stringify({ version: 1, bot: { name: " " } }))).toMatchObject({ ok: false });
    expect(parseBotExport(JSON.stringify({ version: 1 }))).toMatchObject({ ok: false });
    expect(parseBotExport("not json")).toMatchObject({ ok: false, error: expect.stringContaining("JSON") });
    expect(parseBotExport("[]")).toMatchObject({ ok: false });
    expect(parseBotExport(JSON.stringify({ version: 1, bot: { name: "x", instructions: 3 } }))).toMatchObject({ ok: false });
  });
});

describe("copyName", () => {
  it("suffixes and stays within the name limit", () => {
    expect(copyName("Planner")).toBe("Planner (copy)");
    expect(Array.from(copyName("x".repeat(80))).length).toBe(80);
    expect(copyName("x".repeat(80)).endsWith(" (copy)")).toBe(true);
  });
});
