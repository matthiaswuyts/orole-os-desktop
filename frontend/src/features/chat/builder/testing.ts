import { vi } from "vitest";
import type { Bot, BotConfig, Caps, ChatToolInfo, Model, ProviderStatus } from "@/mygo";

/**
 * The mocked MyGo bridge shared by the builder tests. Install with
 * `vi.mock("@/mygo", async () => (await import("./testing")).mygoMock)`; the dynamic import is required because
 * vi.mock is hoisted above static imports.
 */
const listeners = new Map<string, Set<(payload: never) => void>>();

function event(name: string) {
  return {
    on: (listener: (payload: never) => void) => {
      const set = listeners.get(name) ?? new Set();
      set.add(listener);
      listeners.set(name, set);
      return () => { set.delete(listener); };
    },
  };
}

/** Delivers a bridge event to every current subscriber. */
export function emit(name: "chatMessage" | "chatMessageDelta" | "chatRun" | "chatMemory" | "chatBot" | "llmChanged", payload: unknown) {
  for (const listener of [...(listeners.get(name) ?? [])]) (listener as (p: unknown) => void)(payload);
}

export const bridge = {
  desktop: { openSettings: vi.fn() },
  chat: {
    bot: vi.fn(), bots: vi.fn(), saveBot: vi.fn(), archiveBot: vi.fn(), exportBot: vi.fn(), importBot: vi.fn(), draftSend: vi.fn(), cancel: vi.fn(), send: vi.fn(),
    beginAttachment: vi.fn(), attachmentChunk: vi.fn(), commitAttachment: vi.fn(), discardAttachment: vi.fn(),
    memoryScopes: vi.fn(), createMemoryScope: vi.fn(), deleteMemoryScope: vi.fn(), memoryEntries: vi.fn(), saveMemoryEntry: vi.fn(), deleteMemoryEntry: vi.fn(), resolveMemory: vi.fn(),
    knowledgeFiles: vi.fn(), addKnowledge: vi.fn(), removeKnowledge: vi.fn(), skillProposal: vi.fn(), resolveSkill: vi.fn(), tools: vi.fn(),
  },
  mcp: { list: vi.fn() },
  skills: { list: vi.fn() },
  llm: { providers: vi.fn(), defaults: vi.fn(), models: vi.fn() },
};

export const mygoMock = {
  Chat: bridge.chat,
  MCP: bridge.mcp,
  Skills: bridge.skills,
  LLM: bridge.llm,
  Daemon: { health: vi.fn() },
  Desktop: bridge.desktop,
  events: {
    chatMessage: event("chatMessage"), chatMessageDelta: event("chatMessageDelta"), chatRun: event("chatRun"), chatMemory: event("chatMemory"), chatBot: event("chatBot"),
    llmChanged: event("llmChanged"), daemonState: event("daemonState"),
  },
};

export const caps = (over: Partial<Caps> = {}): Caps => ({ tools: true, vision: false, pdf: false, reasoning: false, hostedTools: [], deepResearch: false, contextWindow: 0, ...over });

export const provider = (over: Partial<ProviderStatus> = {}): ProviderStatus => ({ id: "openrouter", name: "OpenRouter", kind: "builtin", connected: true, ...over });

export const model = (id: string, over: Partial<Model> = {}): Model => ({ id, name: id, caps: caps(), reasoning: [], ...over });

export const config = (over: Partial<BotConfig> = {}): BotConfig => ({
  tools: [], mcpServers: [], skills: [], memory: [], limits: { maxSteps: 0, maxToolCalls: 0 }, ...over,
});

export const bot = (id: string, over: Partial<Bot> = {}): Bot => ({
  id, name: `Bot ${id}`, avatar: "", color: "blue", description: "", instructions: "", provider: "", model: "", reasoning: "",
  config: config(), createdAt: 1, updatedAt: 1, running: false, ...over,
});

export const toolInfo = (id: string, over: Partial<ChatToolInfo> = {}): ChatToolInfo => ({ id, name: id, description: `${id} description`, available: true, ...over });

/** Resets every bridge mock and gives each call a harmless default. */
export function resetBridge() {
  for (const group of Object.values(bridge)) for (const fn of Object.values(group)) fn.mockReset();
  bridge.chat.bots.mockResolvedValue([]);
  bridge.chat.tools.mockResolvedValue([]);
  bridge.chat.memoryScopes.mockResolvedValue([]);
  bridge.chat.memoryEntries.mockResolvedValue([]);
  bridge.chat.knowledgeFiles.mockResolvedValue([]);
  bridge.chat.cancel.mockResolvedValue(undefined);
  bridge.desktop.openSettings.mockResolvedValue(undefined);
  bridge.mcp.list.mockResolvedValue([]);
  bridge.skills.list.mockResolvedValue([]);
  bridge.llm.providers.mockResolvedValue([provider()]);
  bridge.llm.defaults.mockResolvedValue({ chatModel: "openrouter/gpt-x", memoryModel: "" });
  bridge.llm.models.mockResolvedValue([model("gpt-x", { name: "GPT X" }), model("reason-1", { name: "Reasoner", reasoning: ["low", "high"] })]);
}
