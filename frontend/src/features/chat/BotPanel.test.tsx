import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
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
    expect(screen.queryByText("All changes saved.")).toBeNull();
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

  it("adds a note with Ctrl+Enter and keeps the text with the reason when the daemon refuses it", async () => {
    bridge.chat.memoryScopes.mockResolvedValue([{ id: "s-bot", name: "Travel", kind: "bot", count: 0, used: 0, limit: 100 }]);
    bridge.chat.saveMemoryEntry.mockRejectedValueOnce(new Error("bad_request: core memory is full"));
    render(<BotPanel bot={bot("b1", { name: "Travel" })} />);
    openPage("Memories");
    fireEvent.click(await screen.findByRole("button", { name: "Add memory" }));
    const box = screen.getByRole("textbox", { name: "New memory" });
    fireEvent.change(box, { target: { value: "  Prefers aisle seats " } });
    fireEvent.keyDown(box, { key: "Enter", ctrlKey: true });
    expect(await screen.findByRole("alert")).toHaveTextContent(/^core memory is full$/);
    expect(screen.getByRole("textbox", { name: "New memory" })).toHaveValue("  Prefers aisle seats ");
    bridge.chat.saveMemoryEntry.mockResolvedValueOnce({ id: "e9", scope: "s-bot", kind: "core", text: "Prefers aisle seats", trust: "user", author: "user", pending: false, createdAt: 3, updatedAt: 3 });
    fireEvent.keyDown(box, { key: "Enter", ctrlKey: true });
    await waitFor(() => expect(screen.queryByRole("textbox", { name: "New memory" })).toBeNull());
    expect(bridge.chat.saveMemoryEntry).toHaveBeenLastCalledWith({ id: "", scope: "s-bot", kind: "core", text: "Prefers aisle seats" });
  });

  it("groups notes by how the bot uses them, edits one in place and forgets everything after asking", async () => {
    bridge.chat.memoryScopes.mockResolvedValue([{ id: "s-bot", name: "Travel", kind: "bot", count: 3, used: 40, limit: 2200 }]);
    bridge.chat.memoryEntries.mockResolvedValue([
      { id: "c1", scope: "s-bot", kind: "core", text: "Wants short answers", trust: "user", author: "user", pending: false, createdAt: 1, updatedAt: 1 },
      { id: "f1", scope: "s-bot", kind: "fact", text: "Lives in Oslo", trust: "bot", author: "bot:b1", pending: false, createdAt: 2, updatedAt: 2 },
      { id: "p1", scope: "s-bot", kind: "fact", text: "Owns a dog", trust: "bot", author: "bot:b1", pending: true, createdAt: 3, updatedAt: 3 },
    ]);
    bridge.chat.saveMemoryEntry.mockResolvedValue({ id: "c1", scope: "s-bot", kind: "core", text: "Wants very short answers", trust: "user", author: "user", pending: false, createdAt: 1, updatedAt: 4 });
    bridge.chat.deleteMemoryEntry.mockResolvedValue(undefined);
    bridge.chat.resolveMemory.mockResolvedValue(undefined);
    render(<BotPanel bot={bot("b1", { name: "Travel" })} />);
    openPage("Memories");
    const always = await screen.findByRole("region", { name: "Always in mind" });
    expect(always).toHaveTextContent("Wants short answers");
    expect(always).toHaveTextContent("40 / 2,200 characters");
    expect(screen.getByRole("region", { name: "Recalled when relevant" })).toHaveTextContent("Lives in Oslo");
    expect(screen.getByRole("region", { name: "Waiting for approval" })).toHaveTextContent("Owns a dog");

    fireEvent.click(screen.getByRole("button", { name: "Edit: Wants short answers" }));
    const box = screen.getByRole("textbox", { name: "Edit memory: Wants short answers" });
    expect(screen.getByRole("button", { name: /^Save/ })).toBeDisabled();
    fireEvent.change(box, { target: { value: "Wants very short answers" } });
    fireEvent.click(screen.getByRole("button", { name: /^Save/ }));
    await waitFor(() => expect(bridge.chat.saveMemoryEntry).toHaveBeenCalledWith({ id: "c1", scope: "s-bot", kind: "core", text: "Wants very short answers" }));

    fireEvent.click(screen.getByRole("button", { name: "Forget all" }));
    expect(bridge.chat.deleteMemoryEntry).not.toHaveBeenCalled();
    fireEvent.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Forget all" }));
    await waitFor(() => expect(bridge.chat.resolveMemory).toHaveBeenCalledWith({ id: "p1", approve: false }));
    expect(bridge.chat.deleteMemoryEntry).toHaveBeenCalledWith("c1");
    expect(bridge.chat.deleteMemoryEntry).toHaveBeenCalledWith("f1");
  });

  it("does not save an invalid name and says why", async () => {
    render(<BotPanel bot={bot("b1", { name: "Travel" })} />);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: " " } });
    expect(screen.getByText("Give the bot a name.")).toBeInTheDocument();
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
