import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Bot, Message, Session } from "@/mygo";

const api = vi.hoisted(() => ({ get: vi.fn(), sendMessage: vi.fn(), options: vi.fn(), cancelQueuedPrompt: vi.fn(), resumeQueue: vi.fn(), setPermissionMode: vi.fn() }));
// Every daemon event is silent here: tasks change through the store.
vi.mock("@/mygo", () => ({ Agents: api, events: new Proxy({}, { get: () => ({ on: () => () => {} }) }) }));
vi.mock("@/components/ascii/AsciiMark", () => ({ SPACE_PIECES: [], AsciiMark: () => null }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/stores/llm", () => ({ useHasProvider: () => true }));
vi.mock("@/stores/daemon", () => ({ useDaemon: Object.assign((select: (state: { link: string }) => unknown) => select({ link: "connected" }), { subscribe: () => () => {} }) }));
vi.mock("@/features/agents/shared", () => ({ ErrorNote: ({ error }: { error: string | null }) => error ? <p role="alert">{error}</p> : null }));
// The live feed is the Tasks view's own (tested there); here it only has to show which task it follows. The composer is the real one,
// loaded inside the factory because vi.mock factories run before this file's static imports.
vi.mock("@/features/tasks/TaskView", async () => {
  const { Composer } = await import("@/features/tasks/Composer");
  return { TaskView: ({ session }: { session: Session }) => <div><div data-testid="task-feed">{session.id}</div><Composer session={session} /></div> };
});
// The conversation is one reply that started three tasks.
vi.mock("@/features/chat/Conversation", async () => {
  const { MessageParts } = await import("@/features/chat/parts/MessageParts");
  const reply: Pick<Message, "parts" | "status"> = { status: "complete", parts: [
    { type: "text", text: "Starting three tasks." },
    { type: "task", id: "t1", title: "Fix login", name: "claude" },
    { type: "task", id: "t2", title: "Add tests", name: "codex" },
    { type: "task", id: "t3", title: "Write docs", name: "claude" },
  ] };
  return { Conversation: () => <div data-testid="conversation"><MessageParts message={reply} /></div> };
});

import { ChatView } from "./ChatView";
import { useChat } from "@/stores/chat";
import { useAgents } from "@/stores/agents";
import { useHarnessOptionsCache } from "@/stores/harnessOptions";

const bot = (id: string): Bot => ({
  id, name: id, avatar: "", color: "blue", description: "", instructions: "", provider: "", model: "", reasoning: "",
  config: { tools: [], mcpServers: [], skills: [], memory: [], limits: { maxSteps: 0, maxToolCalls: 0 } },
  createdAt: 1, updatedAt: 1, running: false,
});
const session = (id: string, patch: Partial<Session>): Session => ({ id, title: "", task: "", kind: "claude", mode: "structured", machine: "local", live: true, status: "working", pending: [], questions: [], queue: [], updatedAt: "2026-10-12T10:00:00Z", error: "", ...patch }) as Session;

beforeEach(() => {
  vi.clearAllMocks();
  api.options.mockResolvedValue({ harnesses: [] });
  api.sendMessage.mockImplementation(async (id: string) => useAgents.getState().sessions[id]);
  useHarnessOptionsCache.setState({ machines: {} });
  useChat.setState({ bots: [bot("a"), bot("b")], selectedBotId: "a", running: {}, builder: { open: false, botId: null }, settingsOpen: false, taskPaneId: null,
    available: true, ready: true, loading: false, error: null, startSync: vi.fn(() => () => {}), load: vi.fn(async () => {}) });
  useAgents.setState({ loaded: true, available: true, sessions: {
    t1: session("t1", { title: "Fix the login bug", kind: "claude", status: "working" }),
    t2: session("t2", { title: "Add tests", kind: "codex", status: "working" }),
    t3: session("t3", { title: "Write docs", kind: "claude", status: "needs-you" }),
  } });
});
afterEach(cleanup);

const chips = () => within(screen.getByRole("list", { name: "Code tasks" })).getAllByRole("button");

describe("task chips in a bot reply", () => {
  it("shows one chip per started task with its live status, title and harness", () => {
    render(<ChatView />);
    const all = chips();
    expect(all).toHaveLength(3);
    expect(all.map((c) => c.getAttribute("data-status"))).toEqual(["working", "working", "needs-you"]);
    // The live title wins over the one the bot gave at start.
    expect(all[0]).toHaveTextContent("Fix the login bug");
    expect(all[0]).toHaveTextContent("Claude Code");
    expect(all[1]).toHaveTextContent("Codex");
    act(() => useAgents.setState((s) => ({ sessions: { ...s.sessions, t1: { ...s.sessions.t1!, status: "done" } } })));
    expect(chips()[0]).toHaveAttribute("data-status", "done");
  });

  it("opens the task beside the conversation with a composer that talks to that task, and closes back to the chat", async () => {
    render(<ChatView />);
    fireEvent.click(chips()[1]!);
    expect(useChat.getState().taskPaneId).toBe("t2");
    const pane = await screen.findByRole("region", { name: "Add tests" });
    expect(within(pane).getByTestId("task-feed")).toHaveTextContent("t2");
    expect(screen.getByTestId("conversation")).toBeInTheDocument();
    expect(chips()[1]).toHaveAttribute("aria-pressed", "true");

    const input = within(pane).getByLabelText("Message the agent");
    fireEvent.change(input, { target: { value: "Also cover the edge cases" } });
    fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(() => expect(api.sendMessage).toHaveBeenCalledTimes(1));
    expect(api.sendMessage.mock.calls[0]![0]).toBe("t2");
    expect(api.sendMessage.mock.calls[0]![1]).toBe("Also cover the edge cases");

    fireEvent.click(within(pane).getByRole("button", { name: "Close task" }));
    expect(screen.queryByRole("region", { name: "Add tests" })).toBeNull();
    expect(useChat.getState().taskPaneId).toBeNull();
    expect(screen.getByTestId("conversation")).toBeInTheDocument();
  });

  it("fetches a task the agents list does not hold", async () => {
    useAgents.setState({ sessions: {} });
    api.get.mockImplementation(async (id: string) => session(id, { title: `Task ${id}`, kind: "codex", status: "done" }));
    render(<ChatView />);
    await waitFor(() => expect(chips()[0]).toHaveTextContent("Task t1"));
    expect(api.get.mock.calls.map(([id]) => id).toSorted()).toEqual(["t1", "t2", "t3"]);
  });
});

describe("task pane state", () => {
  it("shares the side with the settings panel and belongs to the open conversation", () => {
    const chat = useChat.getState();
    chat.openTaskPane("t1");
    chat.setSettingsOpen(true);
    expect(useChat.getState()).toMatchObject({ settingsOpen: true, taskPaneId: null });
    chat.openTaskPane("t1");
    expect(useChat.getState()).toMatchObject({ settingsOpen: false, taskPaneId: "t1" });
    chat.select("a");
    expect(useChat.getState().taskPaneId).toBe("t1");
    chat.select("b");
    expect(useChat.getState().taskPaneId).toBeNull();
  });
});
