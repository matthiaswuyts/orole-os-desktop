import type { Bot, BotConfig, BotExport, BotInput, BotTool, ScopeAccess } from "@/mygo";
import { BOT_COLORS } from "@/lib/botColors";

/** Limits mirrored from the daemon (internal/chat/api.go, store.go). The daemon validates again. */
export const LIMITS = {
  name: 80,
  /** Avatars show at most two graphemes; the daemon accepts up to 16 characters. */
  avatarGraphemes: 2,
  avatarChars: 16,
  description: 500,
  instructions: 32 * 1024,
  steps: { min: 1, max: 30, default: 8 },
  toolCalls: { min: 1, max: 60, default: 20 },
  maxResults: { min: 1, max: 10 },
  maxContentTokens: { min: 1, max: 100_000 },
  domains: 20,
  mcpTools: 100,
  skills: 50,
  delegates: 20,
} as const;

export const TOOL_IDS = { webSearch: "web_search", webFetch: "web_fetch", datetime: "datetime", research: "research", knowledge: "knowledge_search" } as const;

/** Editable settings of one enabled tool. Numbers stay text so a half-typed value is not lost. */
export interface ToolSettings {
  maxResults: string;
  maxContentTokens: string;
  allowed: string[];
  blocked: string[];
  /** Params this form does not edit, carried through unchanged. */
  rest: Record<string, unknown>;
}

export interface FormState {
  name: string;
  avatar: string;
  color: string;
  description: string;
  instructions: string;
  /** "<provider>/<model>"; empty = use the defaults. */
  model: string;
  reasoning: string;
  /** Enabled tools; presence in the record means enabled. */
  tools: Record<string, ToolSettings>;
  mcpTools: string[];
  skills: string[];
  delegateTo: string[];
  /** Grants beyond the private scope. */
  memory: ScopeAccess[];
  maxSteps: string;
  maxToolCalls: string;
  memoryApproval: boolean;
}

export const emptyToolSettings = (): ToolSettings => ({ maxResults: "", maxContentTokens: "", allowed: [], blocked: [], rest: {} });

export const initialForm = (): FormState => ({
  name: "", avatar: "", color: BOT_COLORS[0], description: "", instructions: "", model: "", reasoning: "",
  tools: {}, mcpTools: [], skills: [], delegateTo: [], memory: [], maxSteps: "", maxToolCalls: "", memoryApproval: false,
});

export type Action =
  | { type: "patch"; patch: Partial<FormState> }
  | { type: "load"; state: FormState }
  | { type: "tool"; id: string; settings: ToolSettings | null }
  | { type: "toolPatch"; id: string; patch: Partial<ToolSettings> }
  /** Write implies read; clearing read clears write. Neither = no grant. */
  | { type: "access"; scope: string; read: boolean; write: boolean };

export function formReducer(state: FormState, action: Action): FormState {
  switch (action.type) {
    case "patch": return { ...state, ...action.patch };
    case "load": return action.state;
    case "tool": {
      const tools = { ...state.tools };
      if (action.settings) tools[action.id] = action.settings; else delete tools[action.id];
      return { ...state, tools };
    }
    case "toolPatch": {
      const current = state.tools[action.id];
      return current ? { ...state, tools: { ...state.tools, [action.id]: { ...current, ...action.patch } } } : state;
    }
    case "access": {
      const rest = state.memory.filter((a) => a.scope !== action.scope);
      const write = action.write;
      const read = action.read || write;
      return { ...state, memory: read ? [...rest, { scope: action.scope, read, write }] : rest };
    }
  }
}

/* ------------------------------------------------------------------ provider/model ids */

/** Splits "provider/model"; the model id may itself contain slashes. */
export function splitModel(value: string): { provider: string; model: string } {
  const i = value.indexOf("/");
  return i < 0 ? { provider: "", model: "" } : { provider: value.slice(0, i), model: value.slice(i + 1) };
}

/* ------------------------------------------------------------------ domains */

/** Why `input` is not a usable host name, or the normalized host name. Mirrors the daemon's filter rules. */
export function parseDomain(input: string): { ok: true; domain: string } | { ok: false; error: string } {
  const domain = input.trim().toLowerCase().replace(/\.$/, "");
  if (!domain) return { ok: false, error: "Enter a domain such as example.com." };
  if (/[/:@\s?#]/.test(domain)) return { ok: false, error: "Enter only the host name, such as example.com (no https://, path or port)." };
  if (!domain.includes(".")) return { ok: false, error: "A domain needs a dot, such as example.com." };
  if (/^\d+(\.\d+){3}$/.test(domain) || domain.includes("[")) return { ok: false, error: "IP addresses are not allowed; use a host name." };
  if (domain.length > 253) return { ok: false, error: "That domain is too long." };
  for (const label of domain.split(".")) {
    if (!label || label.length > 63 || label.startsWith("-") || label.endsWith("-") || !/^[a-z0-9-]+$/.test(label)) {
      return { ok: false, error: "Use letters, digits and hyphens in each part of the domain." };
    }
  }
  return { ok: true, domain };
}

/* ------------------------------------------------------------------ conversion */

function stringList(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((v): v is string => typeof v === "string") : [];
}

function numberText(value: unknown): string {
  return typeof value === "number" && Number.isFinite(value) && value > 0 ? String(Math.trunc(value)) : "";
}

function toolSettings(tool: BotTool): ToolSettings {
  const { max_results, max_content_tokens, allowed_domains, blocked_domains, ...rest } = tool.params ?? {};
  return { maxResults: numberText(max_results), maxContentTokens: numberText(max_content_tokens), allowed: stringList(allowed_domains), blocked: stringList(blocked_domains), rest };
}

/** The editable form of a bot (or an import) with missing parts defaulted. */
export function formFromInput(bot: Pick<BotInput, "name" | "avatar" | "color" | "description" | "instructions" | "provider" | "model" | "reasoning"> & { config?: Partial<BotConfig> }): FormState {
  const config = bot.config ?? {};
  const limit = (n: number | undefined) => (n && n > 0 ? String(n) : "");
  return {
    name: bot.name, avatar: bot.avatar, color: bot.color || BOT_COLORS[0], description: bot.description, instructions: bot.instructions,
    model: bot.provider && bot.model ? `${bot.provider}/${bot.model}` : "",
    reasoning: bot.reasoning,
    tools: Object.fromEntries((config.tools ?? []).map((t) => [t.id, toolSettings(t)])),
    mcpTools: [...(config.mcpTools ?? [])], skills: [...(config.skills ?? [])], delegateTo: [...(config.delegateTo ?? [])],
    memory: (config.memory ?? []).map((a) => ({ ...a })),
    maxSteps: limit(config.limits?.maxSteps), maxToolCalls: limit(config.limits?.maxToolCalls),
    memoryApproval: config.memoryApproval ?? false,
  };
}

export const formFromBot = (bot: Bot): FormState => formFromInput(bot);

const integer = (text: string): number => (/^\d+$/.test(text.trim()) ? Number(text.trim()) : 0);

/** Params for a tool, omitting everything left empty; invalid numbers are left out (validation reports them). */
function toolParams(id: string, s: ToolSettings): Record<string, unknown> | undefined {
  const params: Record<string, unknown> = { ...s.rest };
  if (id === TOOL_IDS.webSearch) {
    const n = integer(s.maxResults);
    if (n > 0) params.max_results = n;
  }
  if (id === TOOL_IDS.webFetch) {
    const n = integer(s.maxContentTokens);
    if (n > 0) params.max_content_tokens = n;
  }
  if (id === TOOL_IDS.webSearch || id === TOOL_IDS.webFetch) {
    if (s.allowed.length) params.allowed_domains = s.allowed;
    if (s.blocked.length) params.blocked_domains = s.blocked;
  }
  return Object.keys(params).length ? params : undefined;
}

/** The payload for Chat.saveBot / Chat.draftSend. Pass an empty id to create. */
export function toInput(form: FormState, id: string): BotInput {
  const { provider, model } = splitModel(form.model);
  return {
    id,
    name: form.name.trim(),
    avatar: form.avatar.trim(),
    color: form.color,
    description: form.description.trim(),
    instructions: form.instructions,
    provider, model,
    reasoning: form.reasoning,
    config: {
      tools: Object.entries(form.tools).map(([toolId, s]) => {
        const params = toolParams(toolId, s);
        return params ? { id: toolId, params } : { id: toolId };
      }),
      mcpTools: form.mcpTools,
      skills: form.skills,
      delegateTo: form.delegateTo,
      memory: form.memory,
      limits: { maxSteps: integer(form.maxSteps), maxToolCalls: integer(form.maxToolCalls) },
      memoryApproval: form.memoryApproval,
    },
  };
}

/** A stable text of the form for unsaved-change detection (independent of toggle order). */
export function fingerprint(form: FormState): string {
  return JSON.stringify({
    ...form,
    tools: Object.entries(form.tools).sort(([a], [b]) => a.localeCompare(b)),
    memory: form.memory.toSorted((a, b) => a.scope.localeCompare(b.scope)),
    mcpTools: form.mcpTools.toSorted(), skills: form.skills.toSorted(), delegateTo: form.delegateTo.toSorted(),
  });
}

/* ------------------------------------------------------------------ validation */

export type TabId = "persona" | "model" | "tools" | "memory" | "knowledge";

/** Problems by field key. Tool fields are "tool:<id>:<field>". */
export type Problems = Partial<Record<string, string>>;

export const graphemeCount = (text: string): number => Array.from(new Intl.Segmenter().segment(text)).length;
const charCount = (text: string): number => Array.from(text).length;

function limit(text: string, label: string, range: { min: number; max: number }): string | undefined {
  const t = text.trim();
  if (!t) return undefined;
  if (!/^\d+$/.test(t) || Number(t) < range.min || Number(t) > range.max) return `${label} must be a whole number from ${range.min} to ${range.max}, or empty for the default.`;
  return undefined;
}

export function validate(form: FormState): Problems {
  const problems: Problems = {};
  const name = form.name.trim();
  if (!name) problems.name = "Give the bot a name.";
  else if (charCount(name) > LIMITS.name) problems.name = `Names are limited to ${LIMITS.name} characters.`;
  // eslint-disable-next-line no-control-regex
  else if (/[\u0000-\u001f\u007f]/.test(name)) problems.name = "The name contains invalid characters.";
  const avatar = form.avatar.trim();
  if (avatar && (graphemeCount(avatar) > LIMITS.avatarGraphemes || charCount(avatar) > LIMITS.avatarChars)) problems.avatar = `Use one emoji or up to ${LIMITS.avatarGraphemes} letters.`;
  if (charCount(form.description.trim()) > LIMITS.description) problems.description = `The description is limited to ${LIMITS.description} characters.`;
  if (charCount(form.instructions) > LIMITS.instructions) problems.instructions = `Instructions are limited to ${LIMITS.instructions.toLocaleString("en-US")} characters.`;
  const steps = limit(form.maxSteps, "Max steps", LIMITS.steps);
  if (steps) problems.maxSteps = steps;
  const calls = limit(form.maxToolCalls, "Max tool calls", LIMITS.toolCalls);
  if (calls) problems.maxToolCalls = calls;
  for (const [id, s] of Object.entries(form.tools)) {
    const results = limit(s.maxResults, "Max results", LIMITS.maxResults);
    if (results) problems[`tool:${id}:maxResults`] = results;
    const content = limit(s.maxContentTokens, "Max content size", LIMITS.maxContentTokens);
    if (content) problems[`tool:${id}:maxContentTokens`] = content;
    if (s.allowed.length > LIMITS.domains) problems[`tool:${id}:allowed`] = `At most ${LIMITS.domains} allowed domains.`;
    if (s.blocked.length > LIMITS.domains) problems[`tool:${id}:blocked`] = `At most ${LIMITS.domains} blocked domains.`;
  }
  if (form.mcpTools.length > LIMITS.mcpTools) problems.mcpTools = `At most ${LIMITS.mcpTools} MCP tools can be enabled.`;
  if (form.skills.length > LIMITS.skills) problems.skills = `At most ${LIMITS.skills} skills can be enabled.`;
  if (form.delegateTo.length > LIMITS.delegates) problems.delegateTo = `A bot can ask at most ${LIMITS.delegates} other bots.`;
  return problems;
}

/** The tab that holds the field a problem key refers to. */
export function tabOfProblem(key: string): TabId {
  if (key.startsWith("tool:") || key === "mcpTools" || key === "skills" || key === "delegateTo") return "tools";
  if (key === "maxSteps" || key === "maxToolCalls") return "model";
  return "persona";
}

/* ------------------------------------------------------------------ import */

export type ParsedExport = { ok: true; value: BotExport } | { ok: false; error: string };

/** Parses a pasted or selected bot export. Only version 1 is understood; the daemon validates the content again. */
export function parseBotExport(text: string): ParsedExport {
  let raw: unknown;
  try { raw = JSON.parse(text); } catch { return { ok: false, error: "This is not valid JSON." }; }
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return { ok: false, error: "Expected a bot export object." };
  const doc = raw as Record<string, unknown>;
  if (doc.version !== 1) return { ok: false, error: typeof doc.version === "number" ? `Export version ${doc.version} is not supported; only version 1 can be imported.` : "The export has no version number; only version 1 can be imported." };
  const bot = doc.bot;
  if (!bot || typeof bot !== "object" || Array.isArray(bot)) return { ok: false, error: "The export has no bot." };
  const b = bot as Record<string, unknown>;
  for (const key of ["name", "avatar", "color", "description", "instructions", "provider", "model", "reasoning"] as const) {
    if (b[key] !== undefined && typeof b[key] !== "string") return { ok: false, error: `“${key}” must be text.` };
  }
  if (typeof b.name !== "string" || !b.name.trim()) return { ok: false, error: "The exported bot has no name." };
  const text_ = (key: string) => (typeof b[key] === "string" ? (b[key] as string) : "");
  const config = (b.config && typeof b.config === "object" && !Array.isArray(b.config) ? b.config : {}) as Record<string, unknown>;
  const limits = (config.limits && typeof config.limits === "object" ? config.limits : {}) as Record<string, unknown>;
  const tools = Array.isArray(config.tools)
    ? config.tools.flatMap((t): BotTool[] => {
      if (!t || typeof t !== "object" || typeof (t as BotTool).id !== "string") return [];
      const { id, params } = t as BotTool;
      return [params && typeof params === "object" && !Array.isArray(params) ? { id, params } : { id }];
    })
    : [];
  const memory = Array.isArray(config.memory)
    ? config.memory.flatMap((a): ScopeAccess[] => a && typeof a === "object" && typeof (a as ScopeAccess).scope === "string" ? [{ scope: (a as ScopeAccess).scope, read: (a as ScopeAccess).read === true, write: (a as ScopeAccess).write === true }] : [])
    : [];
  return {
    ok: true,
    value: {
      version: 1,
      bot: {
        id: "", name: b.name, avatar: text_("avatar"), color: text_("color") || BOT_COLORS[0], description: text_("description"), instructions: text_("instructions"),
        provider: text_("provider"), model: text_("model"), reasoning: text_("reasoning"),
        config: {
          tools, mcpTools: stringList(config.mcpTools), skills: stringList(config.skills), delegateTo: stringList(config.delegateTo), memory,
          limits: { maxSteps: typeof limits.maxSteps === "number" ? limits.maxSteps : 0, maxToolCalls: typeof limits.maxToolCalls === "number" ? limits.maxToolCalls : 0 },
          memoryApproval: config.memoryApproval === true,
        },
      },
    },
  };
}

/** Name for a "Save as copy": suffixed and trimmed to the name limit. */
export function copyName(name: string): string {
  const suffix = " (copy)";
  const base = Array.from(name.trim());
  return base.length + suffix.length > LIMITS.name ? `${base.slice(0, LIMITS.name - suffix.length).join("")}${suffix}` : `${base.join("")}${suffix}`;
}
