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
  it("has Persona, Tools and Knowledge tabs and no instructions editor", () => {
    render(<BotPanel bot={bot("b1", { instructions: "Plan trips" })} />);
    expect(screen.getAllByRole("tab").map((t) => t.textContent)).toEqual(["Persona", "Tools", "Knowledge"]);
    expect(screen.queryByRole("textbox", { name: "Instructions" })).toBeNull();
    expect(screen.getByText("Instructions")).toBeInTheDocument();
  });

  it("saves persona and tool changes together, keeping the instructions and the setup flag", async () => {
    const b = bot("b1", { name: "Travel", instructions: "Plan trips", config: { ...bot("x").config, setup: true } });
    saved.mockResolvedValue(b);
    render(<BotPanel bot={b} />);
    expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Trips" } });
    fireEvent.change(screen.getByLabelText("Description"), { target: { value: "Plans trips" } });
    openTab("Tools");
    fireEvent.click(await screen.findByRole("switch", { name: "Date and time" }));
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(saved).toHaveBeenCalledTimes(1));
    expect(saved.mock.calls[0]![0]).toMatchObject({
      id: "b1", name: "Trips", description: "Plans trips", instructions: "Plan trips",
      config: { tools: [{ id: "datetime" }], setup: true },
    });
  });

  it("refuses an empty name before calling the daemon", () => {
    render(<BotPanel bot={bot("b1", { name: "Travel" })} />);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: " " } });
    expect(screen.getByText("Give the bot a name.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save changes" })).toBeDisabled();
    expect(saved).not.toHaveBeenCalled();
  });

  it("discards edits and closes on request", () => {
    render(<BotPanel bot={bot("b1", { name: "Travel" })} />);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Other" } });
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(screen.getByLabelText("Name")).toHaveValue("Travel");
    fireEvent.click(screen.getByRole("button", { name: "Close settings" }));
    expect(useChat.getState().settingsOpen).toBe(false);
  });
});
