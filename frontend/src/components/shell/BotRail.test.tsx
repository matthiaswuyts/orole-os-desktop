import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Bot, BotChange, Message, RunState, Session } from "@/mygo";
import type { ChatState } from "@/stores/chat";

const bridge = vi.hoisted(() => ({
  bots: vi.fn(), clear: vi.fn(), archiveBot: vi.fn(), pinBot: vi.fn(), openSettings: vi.fn(),
  provider: { value: true as boolean | null },
  run: null as ((s: RunState) => void) | null,
  message: null as ((m: Message) => void) | null,
}));

vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/stores/llm", () => ({ useHasProvider: () => bridge.provider.value }));
vi.mock("@/mygo", async (importOriginal) => {
  const actual = await importOriginal<Record<string, unknown>>();
  const none = () => () => {};
  return {
    ...actual,
    Desktop: { openSettings: bridge.openSettings },
    Chat: { bots: bridge.bots, clear: bridge.clear, archiveBot: bridge.archiveBot, pinBot: bridge.pinBot },
    events: {
      chatRun: { on: (f: (s: RunState) => void) => { bridge.run = f; return () => { bridge.run = null; }; } },
      chatMessage: { on: (f: (m: Message) => void) => { bridge.message = f; return () => { bridge.message = null; }; } },
      chatMessageDelta: { on: () => none() },
      chatBot: { on: (_f: (c: BotChange) => void) => none() },
    },
  };
});

import { useAgents } from "@/stores/agents";
import { useChat } from "@/stores/chat";
import { useDaemon } from "@/stores/daemon";
import { useUI } from "@/stores/ui";
import { BotRail } from "./BotRail";

const bot = (id: string, over: Partial<Bot> = {}): Bot => ({
  id, name: id, avatar: "", color: "blue", description: "", instructions: "", provider: "", model: "", reasoning: "",
  config: { tools: [], mcpServers: [], skills: [], memory: [], limits: { maxSteps: 0, maxToolCalls: 0 } },
  createdAt: 1, updatedAt: 1, running: false, ...over,
});
const NOW = Date.parse("2026-10-09T12:00:00Z");
const bots = [
  bot("travel", { name: "Travel", avatar: "✈️", lastMessage: { role: "user", text: "Find flights to Oslo", at: NOW - 5 * 60_000 } }),
  bot("coder", { name: "Coder", color: "violet", lastMessage: { role: "assistant", text: "Refactor done", at: NOW - 3 * 3_600_000 }, description: "Writes Go" }),
  bot("quiet", { name: "Quiet" }),
];
const loaded = (over: Partial<ChatState> = {}) =>
  useChat.setState({ bots, selectedBotId: null, running: {}, available: true, ready: true, loading: false, error: null, builder: { open: false, botId: null }, settingsOpen: false, startSync: vi.fn(() => () => {}), ...over });

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(NOW);
  bridge.bots.mockReset(); bridge.clear.mockReset(); bridge.archiveBot.mockReset(); bridge.pinBot.mockReset(); bridge.openSettings.mockReset().mockResolvedValue(undefined);
  bridge.provider.value = true;
  localStorage.clear();
  useDaemon.setState({ link: "connected" });
  useUI.setState({ view: "chat" });
  useAgents.setState({ sessions: {} });
  loaded();
});
afterEach(() => { cleanup(); vi.useRealTimers(); });

describe("BotRail states", () => {
  it("asks for the desktop app in the browser preview", () => {
    loaded({ available: false, ready: false });
    render(<BotRail />);
    expect(screen.getByText(/needs the desktop app/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /New bot/ })).toBeNull();
  });

  it("shows a skeleton while the first list loads", () => {
    loaded({ ready: false, loading: true, bots: [] });
    render(<BotRail />);
    expect(screen.getByRole("status", { name: "Loading bots" })).toBeInTheDocument();
  });

  it("shows the failure with a retry that reloads", () => {
    const load = vi.fn();
    loaded({ ready: false, error: "daemon offline", bots: [], load });
    render(<BotRail />);
    fireEvent.click(screen.getByRole("button", { name: /Could not load bots: daemon offline/ }));
    expect(load).toHaveBeenCalledTimes(1);
  });

  it("offers a provider setup card instead of bots when no provider is connected", () => {
    bridge.provider.value = false;
    loaded({ bots: [] });
    render(<BotRail />);
    expect(screen.queryByRole("button", { name: /Create your first bot/ })).toBeNull();
    expect(screen.getByRole("button", { name: /^New bot$/ })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: /Connect a provider/ }));
    expect(bridge.openSettings).toHaveBeenCalledWith("ai"); // settings open in their own window
    expect(useUI.getState().settingsSection).toBe("ai");
  });

  it("invites creating the first bot, never showing sample bots", () => {
    loaded({ bots: [] });
    render(<BotRail />);
    expect(screen.queryByRole("list", { name: "Bots" })).toBeNull();
    expect(screen.queryByRole("button", { name: /^New bot$/ })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /Create your first bot/ }));
    expect(useChat.getState().builder).toEqual({ open: true, botId: null });
  });

  it("has a New bot action and no thread or history controls", () => {
    render(<BotRail />);
    fireEvent.click(screen.getByRole("button", { name: /^New bot$/ }));
    expect(useChat.getState().builder).toEqual({ open: true, botId: null });
    expect(screen.queryByText(/new chat/i)).toBeNull();
    expect(screen.queryByText(/history/i)).toBeNull();
  });
});

describe("BotRail rows", () => {
  it("shows name, one-line preview and relative time for each bot, newest conversation first", () => {
    render(<BotRail />);
    const rows = screen.getAllByRole("button", { name: /^(Travel|Coder|Quiet)/ });
    expect(rows).toHaveLength(3);
    expect(within(rows[0]!).getByText("Travel")).toBeInTheDocument();
    expect(within(rows[0]!).getByText("You: Find flights to Oslo")).toBeInTheDocument();
    expect(within(rows[0]!).getByText("5m")).toBeInTheDocument();
    expect(within(rows[1]!).getByText("Refactor done")).toBeInTheDocument();
    expect(within(rows[1]!).getByText("3h")).toBeInTheDocument();
    expect(within(rows[2]!).getByText("No messages yet")).toBeInTheDocument();
  });

  it("marks a bot whose last message is an unanswered question as waiting, on its avatar", () => {
    loaded({ bots: [bot("coder", { name: "Coder", lastMessage: { role: "assistant", text: "What for?", at: NOW, awaiting: true } }), bot("quiet", { name: "Quiet" })] });
    render(<BotRail />);
    const row = screen.getByRole("button", { name: /^Coder/ });
    expect(within(row).getByText("Waiting for your answer")).toBeInTheDocument();
    expect(within(screen.getByRole("button", { name: /^Quiet/ })).queryByText(/Responding|Waiting/)).toBeNull();
  });

  it("marks only a running bot with a Responding indicator, updated live", () => {
    loaded({ running: { coder: true } });
    render(<BotRail />);
    expect(screen.getAllByText("Responding")).toHaveLength(1);
    expect(within(screen.getByRole("button", { name: /^Coder/ })).getByText("Responding")).toBeInTheDocument();
    act(() => useChat.setState({ running: {} }));
    expect(screen.queryByText("Responding")).toBeNull();
  });

  it("draws no activity indicator at all while every bot is idle", () => {
    const { container } = render(<BotRail />);
    expect(container.querySelector("[data-status]")).toBeNull();
    expect(screen.queryByText(/Responding|Running a code task|Waiting/)).toBeNull();
  });

  it("shows a working indicator while a bot owns a working code task, and clears it when the task finishes, fails or is stopped", () => {
    const task = (status: string, owner = "bot:coder") => ({ id: "t1", status, owner, review: "" }) as unknown as Session;
    const row = () => screen.getByRole("button", { name: /^Coder/ });
    render(<BotRail />);
    act(() => useAgents.setState({ sessions: { t1: task("working") } }));
    expect(within(row()).getByText("Running a code task")).toBeInTheDocument();
    expect(row().querySelector("[data-status=working]")).not.toBeNull();
    // Only the owner shows it.
    expect(within(screen.getByRole("button", { name: /^Travel/ })).queryByText("Running a code task")).toBeNull();
    for (const status of ["done", "failed", "stopped", "lost", "idle", "needs-you"]) {
      act(() => useAgents.setState({ sessions: { t1: task(status) } }));
      expect(row().querySelector("[data-status]")).toBeNull();
    }
    act(() => useAgents.setState({ sessions: { t1: task("working") } }));
    act(() => useAgents.setState({ sessions: {} })); // removed
    expect(row().querySelector("[data-status]")).toBeNull();
    // A task the user started belongs to no bot.
    act(() => useAgents.setState({ sessions: { t1: task("working", "") } }));
    expect(screen.queryByText("Running a code task")).toBeNull();
  });

  it("shows responding while a reply streams and nothing once it ends, even with an old finished task", () => {
    useAgents.setState({ sessions: { t1: { id: "t1", status: "done", owner: "bot:coder", review: "" } as unknown as Session } });
    render(<BotRail />);
    act(() => useChat.setState({ running: { coder: true } }));
    expect(screen.getByRole("button", { name: /^Coder/ }).querySelector("[data-status=responding]")).not.toBeNull();
    act(() => useChat.setState({ running: { coder: false } }));
    expect(screen.getByRole("button", { name: /^Coder/ }).querySelector("[data-status]")).toBeNull();
  });

  it("selecting a row opens the chat view and marks it current", () => {
    useUI.setState({ view: "settings" });
    render(<BotRail />);
    fireEvent.click(screen.getByRole("button", { name: /^Coder/ }));
    expect(useChat.getState().selectedBotId).toBe("coder");
    expect(useUI.getState().view).toBe("chat");
    expect(screen.getByRole("button", { name: /^Coder/ })).toHaveAttribute("aria-current", "true");
    expect(screen.getByRole("button", { name: /^Travel/ })).not.toHaveAttribute("aria-current");
  });

  it("shows no current bot on the Automations view, and a click returns to that bot's chat", () => {
    useUI.setState({ view: "automations" });
    loaded({ selectedBotId: "coder" });
    render(<BotRail />);
    expect(screen.getByRole("heading", { name: "Bots" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^Coder/ })).not.toHaveAttribute("aria-current");
    fireEvent.click(screen.getByRole("button", { name: /^Coder/ }));
    expect(useUI.getState()).toMatchObject({ view: "chat" });
    expect(screen.getByRole("button", { name: /^Coder/ })).toHaveAttribute("aria-current", "true");
  });

  it("New bot from the Automations view opens the builder in the chat view", () => {
    useUI.setState({ view: "automations" });
    render(<BotRail />);
    fireEvent.click(screen.getByRole("button", { name: /^New bot$/ }));
    expect(useChat.getState().builder).toEqual({ open: true, botId: null });
    expect(useUI.getState().view).toBe("chat");
  });

  it("updates previews and the Responding dot from live events through the real sync", async () => {
    bridge.bots.mockResolvedValue(bots);
    // Restore the real store actions, which loaded() stubbed out.
    useChat.setState({ ...useChat.getInitialState(), available: true }, true);
    render(<BotRail />);
    expect(await screen.findByRole("button", { name: /^Travel/ })).toBeInTheDocument();
    act(() => { bridge.run!({ botId: "quiet", running: true }); });
    expect(within(screen.getByRole("button", { name: /^Quiet/ })).getByText("Responding")).toBeInTheDocument();
    act(() => { bridge.message!({ id: "m1", botId: "quiet", seq: 1, role: "assistant", author: "bot:quiet", parts: [{ type: "text", text: "Hello from Quiet" }], status: "running", createdAt: NOW - 1000 }); });
    const rows = screen.getAllByRole("button", { name: /^(Travel|Coder|Quiet)/ });
    expect(rows[0]).toHaveAccessibleName(/^Quiet/);
    expect(within(rows[0]!).getByText("Hello from Quiet")).toBeInTheDocument();
  });
});

describe("BotRail keyboard", () => {
  it("uses a roving tabindex that follows the selected bot", () => {
    loaded({ selectedBotId: "coder" });
    render(<BotRail />);
    const tabbable = screen.getAllByRole("button", { name: /^(Travel|Coder|Quiet)/ }).filter((row) => row.tabIndex === 0);
    expect(tabbable).toHaveLength(1);
    expect(tabbable[0]).toHaveAccessibleName(/^Coder/);
  });

  it("moves focus with Up/Down/Home/End without wrapping, and Enter selects", () => {
    render(<BotRail />);
    const [first, second, third] = screen.getAllByRole("button", { name: /^(Travel|Coder|Quiet)/ }) as [HTMLElement, HTMLElement, HTMLElement];
    first.focus();
    fireEvent.keyDown(first, { key: "ArrowDown" });
    expect(second).toHaveFocus();
    expect(second.tabIndex).toBe(0);
    expect(first.tabIndex).toBe(-1);
    fireEvent.keyDown(second, { key: "End" });
    expect(third).toHaveFocus();
    fireEvent.keyDown(third, { key: "ArrowDown" });
    expect(third).toHaveFocus();
    fireEvent.keyDown(third, { key: "ArrowUp" });
    expect(second).toHaveFocus();
    fireEvent.keyDown(second, { key: "Home" });
    expect(first).toHaveFocus();
    fireEvent.keyDown(first, { key: "ArrowUp" });
    expect(first).toHaveFocus();
    // Enter on a button is a click.
    fireEvent.click(first);
    expect(useChat.getState().selectedBotId).toBe("travel");
  });
});

describe("BotRail actions", () => {
  it("opens a bot's settings panel", async () => {
    render(<BotRail />);
    fireEvent.contextMenu(screen.getByRole("button", { name: /^Coder/ }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Bot settings" }));
    expect(useChat.getState().settingsOpen).toBe(true);
    expect(useChat.getState().selectedBotId).toBe("coder");
  });

  it("lists pinned bots first, keeps each group's order and marks the pin", () => {
    loaded({ bots: [bots[0]!, bots[1]!, { ...bots[2]!, config: { ...bots[2]!.config, pinned: true } }] });
    render(<BotRail />);
    const rows = screen.getAllByRole("button", { name: /^(Travel|Coder|Quiet)/ }).map((b) => b.textContent);
    expect(rows[0]).toMatch(/^Quiet/);
    expect(rows[1]).toMatch(/^Travel/);
    expect(rows[2]).toMatch(/^Coder/);
    expect(within(screen.getByRole("button", { name: /^Quiet/ })).getByLabelText("Pinned")).toBeInTheDocument();
    expect(within(screen.getByRole("button", { name: /^Travel/ })).queryByLabelText("Pinned")).toBeNull();
  });

  it("pins and unpins from the row menu", async () => {
    bridge.pinBot.mockResolvedValue(undefined);
    loaded({ bots: [bots[0]!, { ...bots[1]!, config: { ...bots[1]!.config, pinned: true } }] });
    render(<BotRail />);
    fireEvent.contextMenu(screen.getByRole("button", { name: /^Travel/ }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Pin to top" }));
    await waitFor(() => expect(bridge.pinBot).toHaveBeenCalledWith({ id: "travel", pinned: true }));
    cleanup();
    render(<BotRail />);
    fireEvent.contextMenu(screen.getByRole("button", { name: /^Coder/ }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Unpin" }));
    await waitFor(() => expect(bridge.pinBot).toHaveBeenLastCalledWith({ id: "coder", pinned: false }));
  });

  it("clears a conversation only after confirmation", async () => {
    bridge.clear.mockResolvedValue(undefined);
    render(<BotRail />);
    fireEvent.contextMenu(screen.getByRole("button", { name: /^Coder/ }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /Clear conversation/ }));
    expect(bridge.clear).not.toHaveBeenCalled();
    fireEvent.click(await screen.findByRole("button", { name: "Clear conversation" }));
    await waitFor(() => expect(bridge.clear).toHaveBeenCalledWith("coder"));
  });

  it("archives only after confirmation and can be cancelled", async () => {
    bridge.archiveBot.mockResolvedValue(undefined);
    render(<BotRail />);
    fireEvent.contextMenu(screen.getByRole("button", { name: /^Travel/ }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /Archive/ }));
    fireEvent.click(await screen.findByRole("button", { name: "Cancel" }));
    expect(bridge.archiveBot).not.toHaveBeenCalled();
    fireEvent.contextMenu(screen.getByRole("button", { name: /^Travel/ }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /Archive/ }));
    fireEvent.click(await screen.findByRole("button", { name: "Archive bot" }));
    await waitFor(() => expect(bridge.archiveBot).toHaveBeenCalledWith("travel"));
  });

  it("opens the same menu from the context-menu gesture", async () => {
    render(<BotRail />);
    fireEvent.contextMenu(screen.getByRole("button", { name: /^Coder/ }));
    expect(await screen.findByRole("menuitem", { name: "Bot settings" })).toBeInTheDocument();
  });
});
