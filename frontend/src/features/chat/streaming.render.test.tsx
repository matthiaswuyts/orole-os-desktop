import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { createElement } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type * as Trigger from "@/features/chat/MentionTrigger";
import type * as Bubble from "@/features/chat/parts/MessageBubble";
import type { Bot, BotChange, Message, RunState } from "@/mygo";

/** Render-count harness: a 20 Hz stream of snapshots for one reply must only re-render that reply, not the whole thread. */
const bridge = vi.hoisted(() => ({
  bots: vi.fn(), messages: vi.fn(),
  run: new Set<(s: RunState) => void>(),
  message: new Set<(m: Message) => void>(),
  bot: new Set<(c: BotChange) => void>(),
  views: new Map<string, number>(),
  composer: { renders: 0 },
}));

vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({
  Chat: { bots: bridge.bots, messages: bridge.messages, send: vi.fn(), cancel: vi.fn(), regenerate: vi.fn(), clear: vi.fn() },
  LLM: { models: vi.fn() },
  Skills: { list: () => Promise.resolve([]) },
  Daemon: { health: vi.fn() },
  Shell: { openExternal: vi.fn() },
  events: {
    chatRun: { on: (f: (s: RunState) => void) => { bridge.run.add(f); return () => { bridge.run.delete(f); }; } },
    chatMessage: { on: (f: (m: Message) => void) => { bridge.message.add(f); return () => { bridge.message.delete(f); }; } },
    chatBot: { on: (f: (c: BotChange) => void) => { bridge.bot.add(f); return () => { bridge.bot.delete(f); }; } },
  },
}));
// One ChatMessageView renders per message turn render; one MentionTrigger per Composer render. Counting them counts those renders.
vi.mock("@/features/chat/parts/MessageBubble", async (original) => {
  const actual = await original<typeof Bubble>();
  const Counted = (props: Bubble.ChatMessageViewProps) => {
    bridge.views.set(props.message.id, (bridge.views.get(props.message.id) ?? 0) + 1);
    return createElement(actual.ChatMessageView, props);
  };
  return { ...actual, ChatMessageView: Counted };
});
vi.mock("@/features/chat/MentionTrigger", async (original) => {
  const actual = await original<typeof Trigger>();
  const Counted = (props: Parameters<typeof actual.MentionTrigger>[0]) => {
    bridge.composer.renders++;
    return createElement(actual.MentionTrigger, props);
  };
  return { ...actual, MentionTrigger: Counted };
});

import { Conversation } from "@/features/chat/Conversation";
import { useChat } from "@/stores/chat";
import { useChatDrafts } from "@/stores/chatDrafts";
import { useChatMessages } from "@/stores/chatMessages";
import { useDaemon } from "@/stores/daemon";
import { useLLM } from "@/stores/llm";

const bot = (id: string, over: Partial<Bot> = {}): Bot => ({
  id, name: id === "a" ? "Atlas" : "Beta", avatar: "", color: "blue", description: "", instructions: "", provider: "openrouter", model: "claude", reasoning: "",
  config: { tools: [], mcpTools: [], skills: [], memory: [], limits: { maxSteps: 0, maxToolCalls: 0 } },
  createdAt: 1, updatedAt: 1, running: false, ...over,
});
const msg = (seq: number, over: Partial<Message> = {}): Message => ({
  id: `m${seq}`, botId: "a", seq, role: seq % 2 ? "user" : "assistant", author: seq % 2 ? "user" : "bot:a",
  parts: [{ type: "text", text: `${seq % 2 ? "question" : "answer"} ${seq}` }], status: "complete", createdAt: seq * 10, ...over,
});
const caps = { tools: true, vision: false, pdf: true, reasoning: false, hostedTools: [], deepResearch: false, contextWindow: 0 };

const HISTORY = 40;
const STREAMING = `m${HISTORY + 1}`;
const emit = (message: Message) => { for (const f of [...bridge.message]) f(message); };
const settle = async () => { for (let i = 0; i < 8; i++) await Promise.resolve(); };
const selected = () => useChat.getState().bots.find((b) => b.id === "a")!;

let stopChat = () => {};
beforeEach(() => {
  bridge.views.clear();
  bridge.composer.renders = 0;
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  Element.prototype.scrollTo = () => {};
  useDaemon.setState({ link: "connected" });
  useChat.setState({ ...useChat.getInitialState(), available: true });
  useChatMessages.setState({ byBot: {}, runs: {}, problems: {} });
  useChatDrafts.setState({ drafts: {} });
  useLLM.setState({
    providers: [{ id: "openrouter", name: "OpenRouter", kind: "builtin", connected: true }], ready: true, defaults: { chatModel: "openrouter/claude", memoryModel: "" },
    models: { openrouter: { models: [{ id: "claude", name: "Claude Sonnet", caps, reasoning: [] }], loading: false, error: null } },
  });
  bridge.bots.mockResolvedValue([bot("a"), bot("b")]);
});
afterEach(() => { stopChat(); cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); });

describe("chat streaming render counts", () => {
  it("re-renders only the streaming reply while a reply streams at 20 Hz", async () => {
    const history = Array.from({ length: HISTORY }, (_, i) => msg(i + 1));
    bridge.messages.mockResolvedValue({ messages: [...history, msg(HISTORY + 1, { status: "running", parts: [{ type: "text", text: "w0" }] })], hasMore: false });
    useChat.setState({ selectedBotId: "a" });
    stopChat = useChat.getState().startSync();
    await settle();
    render(<Conversation key="a" bot={selected()} />);
    await screen.findByText("w0");
    await act(async () => { await settle(); });

    let botsChanges = 0;
    const off = useChat.subscribe((state, previous) => { if (state.bots !== previous.bots) botsChanges++; });
    bridge.views.clear();
    bridge.composer.renders = 0;

    vi.useFakeTimers();
    const SECONDS = 10;
    for (let i = 1; i <= SECONDS * 20; i++) {
      act(() => { emit(msg(HISTORY + 1, { status: "running", parts: [{ type: "text", text: `w${"x".repeat(i)}` }] })); });
      act(() => { vi.advanceTimersByTime(50); });
    }
    act(() => { emit(msg(HISTORY + 1, { status: "complete", parts: [{ type: "text", text: "done" }] })); });
    act(() => { vi.advanceTimersByTime(5000); });
    off();

    const others = [...bridge.views].filter(([id]) => id !== STREAMING).reduce((sum, [, n]) => sum + n, 0);
    const streaming = bridge.views.get(STREAMING) ?? 0;
    console.log(`RENDER-COUNTS flood (${SECONDS * 20} snapshots, ${HISTORY} settled turns): bots-array changes=${botsChanges} streaming-turn renders=${streaming} other-turn renders=${others} composer renders=${bridge.composer.renders}`);
    expect(botsChanges).toBeLessThanOrEqual(SECONDS + 2);
    // The reply ending is the one legitimate wave: every turn learns once that the run stopped.
    expect(others).toBeLessThanOrEqual(HISTORY);
    expect(bridge.composer.renders).toBeLessThanOrEqual(3);
    expect(screen.getByText("done")).toBeInTheDocument();
  });

  it("typing in the composer re-renders no message", async () => {
    bridge.messages.mockResolvedValue({ messages: Array.from({ length: HISTORY }, (_, i) => msg(i + 1)), hasMore: false });
    useChat.setState({ selectedBotId: "a" });
    stopChat = useChat.getState().startSync();
    await settle();
    render(<Conversation key="a" bot={selected()} />);
    await screen.findByText("answer 40");
    await act(async () => { await settle(); });
    bridge.views.clear();
    bridge.composer.renders = 0;
    const box = screen.getByRole("textbox", { name: "Message Atlas" });
    for (let i = 1; i <= 30; i++) fireEvent.change(box, { target: { value: "x".repeat(i) } });
    const turns = [...bridge.views.values()].reduce((sum, n) => sum + n, 0);
    console.log(`RENDER-COUNTS typing (30 keystrokes, ${HISTORY} settled turns): turn renders=${turns} composer renders=${bridge.composer.renders}`);
    expect(turns).toBe(0);
  });
});
