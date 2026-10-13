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
// The conversation is one reply that started three tasks and wrote a document.
const reply = vi.hoisted((): Message => ({ id: "m1", botId: "a", seq: 1, role: "assistant", author: "bot:a", createdAt: 1, status: "complete", parts: [
  { type: "text", text: "Starting three tasks." },
  { type: "task", id: "t1", title: "Fix login", name: "claude" },
  { type: "task", id: "t2", title: "Add tests", name: "codex" },
  { type: "task", id: "t3", title: "Write docs", name: "claude" },
  { type: "ui", id: "write_document", name: "document", spec: JSON.stringify({ title: "Rollout plan", content: "# Rollout\n\nShip **Friday**.", version: 2 }) },
] }));
vi.mock("@/features/chat/Conversation", async () => {
  const { MessageParts } = await import("@/features/chat/parts/MessageParts");
  return { Conversation: () => <div data-testid="conversation"><MessageParts message={reply} /></div> };
});

import { ChatView } from "./ChatView";
import { useChat } from "@/stores/chat";
import { useChatMessages } from "@/stores/chatMessages";
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
  useChat.setState({ bots: [bot("a"), bot("b")], selectedBotId: "a", running: {}, builder: { open: false, botId: null }, settingsOpen: false, pane: null,
    available: true, ready: true, loading: false, error: null, startSync: vi.fn(() => () => {}), load: vi.fn(async () => {}) });
  useAgents.setState({ loaded: true, available: true, sessions: {
    t1: session("t1", { title: "Fix the login bug", kind: "claude", status: "working" }),
    t2: session("t2", { title: "Add tests", kind: "codex", status: "working" }),
    t3: session("t3", { title: "Write docs", kind: "claude", status: "needs-you" }),
  } });
});
afterEach(cleanup);

const chips = () => within(screen.getByRole("list", { name: "Code tasks" })).getAllByRole("button");
const pane = () => useChat.getState().pane;

describe("task rows in a bot reply", () => {
  it("shows one row per started task with its live status, title, activity and harness", () => {
    render(<ChatView />);
    const all = chips();
    expect(all).toHaveLength(3);
    expect(all.map((c) => c.getAttribute("data-status"))).toEqual(["running", "running", "needs-you"]);
    // The live title wins over the one the bot gave at start.
    expect(all[0]).toHaveTextContent("Fix the login bug");
    expect(all[0]).toHaveTextContent("Claude Code");
    expect(all[1]).toHaveTextContent("Codex");
    expect(within(all[0]!).getByRole("progressbar", { name: "Fix the login bug progress" })).toHaveAttribute("aria-busy", "true");
    act(() => useAgents.setState((s) => ({ sessions: { ...s.sessions, t1: { ...s.sessions.t1!, subagents: 2 } } })));
    expect(chips()[0]).toHaveTextContent("2 background agents running");
    act(() => useAgents.setState((s) => ({ sessions: { ...s.sessions, t1: { ...s.sessions.t1!, status: "done", subagents: 0 } } })));
    expect(chips()[0]).toHaveAttribute("data-status", "done");
    expect(within(chips()[0]!).getByRole("progressbar")).toHaveAttribute("aria-valuenow", "100");
  });

  it("opens the task beside the conversation with a composer that talks to that task, and closes back to the chat", async () => {
    render(<ChatView />);
    fireEvent.click(chips()[1]!);
    expect(pane()).toEqual({ kind: "task", id: "t2" });
    // The pane chunk is lazy and its body waits for the open animation: allow for a loaded test runner.
    const region = await screen.findByRole("region", { name: "Add tests" }, { timeout: 4000 });
    expect(await within(region).findByTestId("task-feed", {}, { timeout: 4000 })).toHaveTextContent("t2");
    expect(screen.getByTestId("conversation")).toBeInTheDocument();
    expect(chips()[1]).toHaveAttribute("aria-pressed", "true");

    const input = within(region).getByLabelText("Message the agent");
    fireEvent.change(input, { target: { value: "Also cover the edge cases" } });
    fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(() => expect(api.sendMessage).toHaveBeenCalledTimes(1));
    expect(api.sendMessage.mock.calls[0]![0]).toBe("t2");
    expect(api.sendMessage.mock.calls[0]![1]).toBe("Also cover the edge cases");

    fireEvent.click(within(region).getByRole("button", { name: "Close task" }));
    expect(pane()).toBeNull();
    await waitFor(() => expect(screen.queryByRole("region", { name: "Add tests" })).toBeNull());
    expect(screen.getByTestId("conversation")).toBeInTheDocument();
  });

  it("fetches a task the agents list does not hold", async () => {
    useAgents.setState({ sessions: {} });
    api.get.mockImplementation(async (id: string) => session(id, { title: `Task ${id}`, kind: "codex", status: "done" }));
    render(<ChatView />);
    await waitFor(() => expect(chips()[0]).toHaveTextContent("Task t1"));
    expect(api.get.mock.calls.map(([id]) => id).toSorted()).toEqual(["t1", "t2", "t3"]);
  });

  it("opens a written document beside the conversation as markdown, copies it, closes it and switches panes without a remount", async () => {
    const writeText = vi.fn(async () => {});
    Object.assign(navigator, { clipboard: { writeText } });
    useChatMessages.setState({ byBot: { a: { messages: [reply], hasMore: false, loaded: true, loading: false, loadingOlder: false, error: null, browsing: false } } });
    render(<ChatView />);
    fireEvent.click(chips()[0]!);
    const shell = await screen.findByRole("region", { name: "Fix the login bug" });
    const host = shell.closest("[data-slot=side-pane]");
    fireEvent.click(screen.getByRole("button", { name: "Open" }));
    expect(pane()).toEqual({ kind: "document", messageId: "m1", partIndex: 4 });
    const region = await screen.findByRole("region", { name: "Rollout plan" });
    expect(region.closest("[data-slot=side-pane]")).toBe(host);
    expect(within(region).getByRole("heading", { name: "Rollout" })).toBeInTheDocument();
    expect(within(region).getByText("Friday").tagName).toBe("STRONG");
    expect(within(region).getByText("v2")).toBeInTheDocument();
    expect(within(region).getByText("Saved")).toBeInTheDocument();
    fireEvent.click(within(region).getByRole("button", { name: "Copy document" }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith("# Rollout\n\nShip **Friday**."));
    fireEvent.click(within(region).getByRole("button", { name: "Close document" }));
    expect(pane()).toBeNull();
    await waitFor(() => expect(screen.queryByRole("region", { name: "Rollout plan" })).toBeNull());
  });
});

describe("task pane state", () => {
  it("shares the side with the settings panel and belongs to the open conversation", () => {
    const chat = useChat.getState();
    chat.openTaskPane("t1");
    chat.setSettingsOpen(true);
    expect(useChat.getState()).toMatchObject({ settingsOpen: true, pane: null });
    chat.openTaskPane("t1");
    expect(useChat.getState()).toMatchObject({ settingsOpen: false, pane: { kind: "task", id: "t1" } });
    chat.openDocument("m1", 2);
    expect(pane()).toEqual({ kind: "document", messageId: "m1", partIndex: 2 });
    chat.select("a");
    expect(pane()).toEqual({ kind: "document", messageId: "m1", partIndex: 2 });
    chat.select("b");
    expect(pane()).toBeNull();
  });
});
