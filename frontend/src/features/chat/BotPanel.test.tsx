import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", async () => (await import("./builder/testing")).mygoMock);

import { useChat } from "@/stores/chat";
import { useLLM } from "@/stores/llm";
import { BotPanel } from "./BotPanel";
import { bot, bridge, model, provider, resetBridge, toolInfo } from "./builder/testing";

const saved = bridge.chat.saveBot;
const openPage = (name: string) => fireEvent.click(screen.getByRole("button", { name: new RegExp(`^${name}`) }));

beforeEach(() => {
  resetBridge();
  bridge.chat.tools.mockResolvedValue([toolInfo("datetime", { name: "Date and time" })]);
  useChat.setState({ settingsOpen: true, builder: { open: false, botId: null } });
  useLLM.setState({
    providers: [provider()], defaults: { chatModel: "openrouter/gpt-x", memoryModel: "" },
    models: { openrouter: { models: [model("gpt-x", { name: "GPT X" })], loading: false, error: null } }, ready: true,
  });
});
afterEach(cleanup);

describe("bot settings panel", () => {
  it("shows who the bot is, how it thinks, what it knows and what it does", () => {
    render(<BotPanel bot={bot("b1", { name: "Travel", description: "Trip planner", instructions: "Plan trips" })} />);
    expect(screen.getByLabelText("Name")).toHaveValue("Travel");
    expect(screen.queryByLabelText("Title")).toBeNull();
    expect(screen.queryByLabelText("Instructions")).toBeNull();
    expect(screen.queryByText("Plan trips")).toBeNull();
    for (const group of ["Brain", "Knows", "Does"]) expect(screen.getByRole("heading", { name: group })).toBeInTheDocument();
    for (const row of ["Memories", "Skills", "Tools", "Integrations"]) expect(screen.getByRole("button", { name: new RegExp(`^${row}`) })).toBeInTheDocument();
  });

  it("saves name and tool changes on its own, once, keeping the instructions and the setup flag", async () => {
    const b = bot("b1", { name: "Travel", instructions: "Plan trips", config: { ...bot("x").config, setup: true } });
    saved.mockResolvedValue(b);
    render(<BotPanel bot={b} />);
    expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Trips" } });
    openPage("Tools");
    fireEvent.click(await screen.findByRole("switch", { name: "Date and time" }));
    fireEvent.click(screen.getByRole("button", { name: "Back to settings" }));
    expect(saved).not.toHaveBeenCalled();
    await waitFor(() => expect(saved).toHaveBeenCalledTimes(1), { timeout: 3000 });
    expect(saved.mock.calls[0]![0]).toMatchObject({
      id: "b1", name: "Trips", description: "", instructions: "Plan trips",
      config: { tools: [{ id: "datetime" }], setup: true },
    });
    await waitFor(() => expect(screen.getByText("All changes saved.")).toBeInTheDocument());
  });

  it("counts the bot's saved memories and lets the user forget one", async () => {
    bridge.chat.memoryScopes.mockResolvedValue([{ id: "s-bot", name: "Travel", kind: "bot", count: 2, used: 0, limit: 100 }]);
    bridge.chat.memoryEntries.mockResolvedValue([
      { id: "e1", scope: "s-bot", kind: "fact", text: "Prefers window seats", trust: "user", author: "user", pending: false, createdAt: 1, updatedAt: 1 },
      { id: "e2", scope: "s-bot", kind: "fact", text: "Lives in Oslo", trust: "bot", author: "bot:b1", pending: true, createdAt: 2, updatedAt: 2 },
    ]);
    bridge.chat.deleteMemoryEntry.mockResolvedValue(undefined);
    render(<BotPanel bot={bot("b1", { name: "Travel" })} />);
    expect(await screen.findByRole("button", { name: /^Memories.*2 saved/ })).toBeInTheDocument();
    openPage("Memories");
    expect(await screen.findByText("Prefers window seats")).toBeInTheDocument();
    expect(screen.getByText("Waiting for approval")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Forget: Prefers window seats" }));
    await waitFor(() => expect(bridge.chat.deleteMemoryEntry).toHaveBeenCalledWith("e1"));
  });

  it("does not save an invalid name and says why", async () => {
    render(<BotPanel bot={bot("b1", { name: "Travel" })} />);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: " " } });
    expect(screen.getByText("Give the bot a name.")).toBeInTheDocument();
    expect(screen.getByText("Fix the highlighted fields to save.")).toBeInTheDocument();
    await new Promise((r) => setTimeout(r, 900));
    expect(saved).not.toHaveBeenCalled();
  });

  it("does not retry a failed save until something changes", async () => {
    saved.mockRejectedValue(new Error("daemon is down"));
    render(<BotPanel bot={bot("b1", { name: "Travel" })} />);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Other" } });
    await waitFor(() => expect(screen.getByText(/daemon is down/)).toBeInTheDocument(), { timeout: 3000 });
    await new Promise((r) => setTimeout(r, 1500));
    expect(saved).toHaveBeenCalledTimes(1);
    saved.mockResolvedValue(bot("b1", { name: "Another" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Another" } });
    await waitFor(() => expect(saved).toHaveBeenCalledTimes(2), { timeout: 3000 });
  });

  it("closes on request", () => {
    render(<BotPanel bot={bot("b1", { name: "Travel" })} />);
    fireEvent.click(screen.getByRole("button", { name: "Close settings" }));
    expect(useChat.getState().settingsOpen).toBe(false);
  });
});
