import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", async () => (await import("./builder/testing")).mygoMock);

import { useChat } from "@/stores/chat";
import { useLLM } from "@/stores/llm";
import { BotPanel } from "./BotPanel";
import { bot, bridge, model, provider, resetBridge, toolInfo } from "./builder/testing";

const saved = bridge.chat.saveBot;
const openTab = (name: string) => fireEvent.click(screen.getByRole("tab", { name }));

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
  it("has Persona and Tools tabs and shows no instructions", () => {
    render(<BotPanel bot={bot("b1", { instructions: "Plan trips" })} />);
    expect(screen.getAllByRole("tab").map((t) => t.textContent)).toEqual(["Persona", "Tools"]);
    expect(screen.queryByRole("textbox", { name: "Instructions" })).toBeNull();
    expect(screen.queryByText("Instructions")).toBeNull();
    expect(screen.queryByText("Plan trips")).toBeNull();
  });

  it("saves persona and tool changes on its own, once, keeping the instructions and the setup flag", async () => {
    const b = bot("b1", { name: "Travel", instructions: "Plan trips", config: { ...bot("x").config, setup: true } });
    saved.mockResolvedValue(b);
    render(<BotPanel bot={b} />);
    expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Discard" })).toBeNull();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Trips" } });
    fireEvent.change(screen.getByLabelText("Description"), { target: { value: "Plans trips" } });
    openTab("Tools");
    fireEvent.click(await screen.findByRole("switch", { name: "Date and time" }));
    expect(saved).not.toHaveBeenCalled();
    await waitFor(() => expect(saved).toHaveBeenCalledTimes(1), { timeout: 3000 });
    expect(saved.mock.calls[0]![0]).toMatchObject({
      id: "b1", name: "Trips", description: "Plans trips", instructions: "Plan trips",
      config: { tools: [{ id: "datetime" }], setup: true },
    });
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("All changes saved."));
  });

  it("does not save an invalid name and says why", async () => {
    render(<BotPanel bot={bot("b1", { name: "Travel" })} />);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: " " } });
    expect(screen.getByText("Give the bot a name.")).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("Fix the highlighted fields to save.");
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
