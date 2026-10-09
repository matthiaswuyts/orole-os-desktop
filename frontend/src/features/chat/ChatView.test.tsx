import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Bot } from "@/mygo";

const env = vi.hoisted(() => ({ desktop: true, provider: true as boolean | null }));

vi.mock("@/lib/backend", () => ({ get inDesktop() { return env.desktop; } }));
vi.mock("@/stores/llm", () => ({ useHasProvider: () => env.provider }));
vi.mock("@/features/chat/Conversation", () => ({ Conversation: ({ bot }: { bot: Bot }) => <div data-testid="conversation">{bot.name}</div> }));
vi.mock("@/features/chat/builder/BotBuilder", () => ({ BotBuilder: ({ botId }: { botId: string | null }) => <div data-testid="builder">{botId ?? "new"}</div> }));

import { ChatView } from "@/features/chat/ChatView";
import { useChat } from "@/stores/chat";
import { useUI } from "@/stores/ui";

const bot = (id: string, name = id): Bot => ({
  id, name, avatar: "", color: "blue", description: "", instructions: "", provider: "", model: "", reasoning: "",
  config: { tools: [], mcpTools: [], skills: [], delegateTo: [], memory: [], limits: { maxSteps: 0, maxToolCalls: 0 }, memoryApproval: false },
  createdAt: 1, updatedAt: 1, running: false,
});
const state = (over: Partial<ReturnType<typeof useChat.getState>> = {}) =>
  useChat.setState({ bots: [], selectedBotId: null, running: {}, builder: { open: false, botId: null }, available: true, ready: true, loading: false, error: null, startSync: vi.fn(() => () => {}), load: vi.fn(async () => {}), ...over });

beforeEach(() => { env.desktop = true; env.provider = true; state(); });
afterEach(cleanup);

describe("ChatView states", () => {
  it("says the desktop app is required in the browser preview", () => {
    env.desktop = false;
    render(<ChatView />);
    expect(screen.getByText("Chat needs the desktop app")).toBeInTheDocument();
  });

  it("sends the user to AI settings when no provider is connected", () => {
    env.provider = false;
    state({ bots: [bot("a")], selectedBotId: "a" });
    render(<ChatView />);
    expect(screen.getByText("Connect an AI provider to start chatting")).toBeInTheDocument();
    expect(screen.queryByTestId("conversation")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Open AI settings" }));
    expect(useUI.getState().view).toBe("settings");
    expect(useUI.getState().settingsSection).toBe("ai");
  });

  it("waits quietly while the provider answer is unknown and the bots load", () => {
    env.provider = null;
    state({ ready: false, loading: true });
    render(<ChatView />);
    expect(screen.getByRole("status")).toHaveTextContent("Loading bots…");
  });

  it("shows a load error with Retry", () => {
    const load = vi.fn(async () => {});
    state({ ready: false, error: "The daemon is offline.", load });
    render(<ChatView />);
    expect(screen.getByText("The daemon is offline.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(load).toHaveBeenCalled();
  });

  it("invites creating the first bot when there are none", () => {
    render(<ChatView />);
    fireEvent.click(screen.getByRole("button", { name: "Create your first bot" }));
    expect(useChat.getState().builder).toEqual({ open: true, botId: null });
  });

  it("asks to pick a bot when bots exist but none is selected", () => {
    state({ bots: [bot("a")] });
    render(<ChatView />);
    expect(screen.getByText("Pick a bot")).toBeInTheDocument();
  });

  it("opens the selected bot's conversation", () => {
    state({ bots: [bot("a", "Atlas"), bot("b", "Beta")], selectedBotId: "b" });
    render(<ChatView />);
    expect(screen.getByTestId("conversation")).toHaveTextContent("Beta");
  });

  it("shows the builder, lazily, while it is open", async () => {
    state({ bots: [bot("a")], selectedBotId: "a", builder: { open: true, botId: "a" } });
    render(<ChatView />);
    expect(await screen.findByTestId("builder")).toHaveTextContent("a");
    expect(screen.queryByTestId("conversation")).toBeNull();
  });

  it("starts and stops syncing the bot list with the view", () => {
    const stop = vi.fn();
    const startSync = vi.fn(() => stop);
    state({ startSync });
    const view = render(<ChatView />);
    expect(startSync).toHaveBeenCalledTimes(1);
    view.unmount();
    expect(stop).toHaveBeenCalledTimes(1);
  });
});
