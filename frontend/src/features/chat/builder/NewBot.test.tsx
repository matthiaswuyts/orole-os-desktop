import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", async () => (await import("./testing")).mygoMock);

import { avatarSeed } from "@/lib/botAvatar";
import { useChat } from "@/stores/chat";
import { useLLM } from "@/stores/llm";
import { NewBot } from "./NewBot";
import { bot, bridge, model, provider, resetBridge } from "./testing";

const saved = bridge.chat.saveBot;
const nameField = () => screen.getByLabelText("Name");

// jsdom has no layout; the virtualized model list renders nothing in a zero-height scroller.
Object.defineProperty(HTMLElement.prototype, "offsetHeight", { configurable: true, value: 320 });
Object.defineProperty(HTMLElement.prototype, "offsetWidth", { configurable: true, value: 560 });

beforeEach(() => {
  resetBridge();
  useChat.setState({ bots: [], selectedBotId: null, builder: { open: true, botId: null } });
  useLLM.setState({
    providers: [provider()], defaults: { chatModel: "openrouter/gpt-x", memoryModel: "" },
    models: { openrouter: { models: [model("gpt-x", { name: "GPT X" })], loading: false, error: null } }, ready: true,
  });
});
afterEach(cleanup);

describe("new bot", () => {
  it("asks only for a name and a model", () => {
    render(<NewBot />);
    expect(nameField()).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Model" })).toBeInTheDocument();
    expect(screen.queryByRole("tab")).toBeNull();
    expect(screen.queryByLabelText("Preview message")).toBeNull();
    expect(screen.queryByLabelText("Avatar")).toBeNull();
  });

  it("creates the bot in setup mode with an avatar seed from its name, opens it", async () => {
    saved.mockResolvedValue(bot("new1", { name: "Trip planner" }));
    render(<NewBot />);
    fireEvent.change(nameField(), { target: { value: "  Trip planner " } });
    fireEvent.click(screen.getByRole("button", { name: "Model" }));
    fireEvent.click(await screen.findByRole("option", { name: /^GPT X/ }));
    fireEvent.click(screen.getByRole("button", { name: "Create bot" }));
    await waitFor(() => expect(saved).toHaveBeenCalledTimes(1));
    expect(saved.mock.calls[0]![0]).toMatchObject({
      id: "", name: "Trip planner", avatar: avatarSeed("Trip planner"), instructions: "", provider: "openrouter", model: "gpt-x",
      config: { tools: [{ id: "web_search" }, { id: "web_fetch" }, { id: "datetime" }, { id: "research" }], setup: true },
    });
    await waitFor(() => expect(useChat.getState().builder.open).toBe(false));
    expect(useChat.getState().selectedBotId).toBe("new1");
    expect(bridge.chat.send).not.toHaveBeenCalled(); // the daemon starts the bot's first message itself
  });

  it("starts on the default chat model, explicitly, when none is picked", async () => {
    saved.mockResolvedValue(bot("new1", { name: "Quick" }));
    render(<NewBot />);
    fireEvent.change(nameField(), { target: { value: "Quick" } });
    fireEvent.click(screen.getByRole("button", { name: "Create bot" }));
    await waitFor(() => expect(saved).toHaveBeenCalled());
    expect(saved.mock.calls[0]![0]).toMatchObject({ provider: "openrouter", model: "gpt-x" });
  });

  it("needs a name before it calls the daemon", () => {
    render(<NewBot />);
    expect(screen.queryByText("Give the bot a name.")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Create bot" }));
    expect(screen.getByText("Give the bot a name.")).toBeInTheDocument();
    expect(saved).not.toHaveBeenCalled();
  });

  it("shows the daemon's refusal verbatim and keeps the name", async () => {
    saved.mockRejectedValue(new Error("bad_request: You can have at most 100 bots. Archive one to add another."));
    render(<NewBot />);
    fireEvent.change(nameField(), { target: { value: "One too many" } });
    fireEvent.click(screen.getByRole("button", { name: "Create bot" }));
    expect(await screen.findByText("You can have at most 100 bots. Archive one to add another.")).toBeInTheDocument();
    expect(nameField()).toHaveValue("One too many");
    expect(useChat.getState().builder.open).toBe(true);
    expect(bridge.chat.send).not.toHaveBeenCalled();
  });
});

describe("import from a file", () => {
  const file = (content: unknown) => new File([typeof content === "string" ? content : JSON.stringify(content)], "bot.json", { type: "application/json" });
  const chooseFile = async (f: File) => {
    fireEvent.click(screen.getByRole("button", { name: "More actions" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /Import from file/ }));
    fireEvent.change(screen.getByLabelText("Choose a bot export file"), { target: { files: [f] } });
  };

  it("refuses an export of another version and says why", async () => {
    render(<NewBot />);
    await chooseFile(file({ version: 2, bot: { name: "Future" } }));
    expect(await screen.findByText(/Export version 2 is not supported; only version 1 can be imported\./)).toBeInTheDocument();
    expect(bridge.chat.importBot).not.toHaveBeenCalled();
  });

  it("refuses text that is not JSON", async () => {
    render(<NewBot />);
    await chooseFile(file("hello"));
    expect(await screen.findByText("This is not valid JSON.")).toBeInTheDocument();
  });

  it("imports a version 1 export as a new bot and opens it", async () => {
    bridge.chat.importBot.mockResolvedValue(bot("imp", { name: "Imported" }));
    render(<NewBot />);
    await chooseFile(file({ version: 1, bot: { name: "Imported", instructions: "Be brief" } }));
    fireEvent.click(await screen.findByRole("button", { name: "Import bot" }));
    await waitFor(() => expect(bridge.chat.importBot).toHaveBeenCalledTimes(1));
    expect(bridge.chat.importBot.mock.calls[0]![0]).toMatchObject({ version: 1, bot: { id: "", name: "Imported", instructions: "Be brief" } });
    await waitFor(() => expect(useChat.getState().selectedBotId).toBe("imp"));
    expect(useChat.getState().builder.open).toBe(false);
  });

  it("shows the daemon's rejection of an import", async () => {
    bridge.chat.importBot.mockRejectedValue(new Error("bad_request: The bot name is too long."));
    render(<NewBot />);
    await chooseFile(file({ version: 1, bot: { name: "Imported" } }));
    fireEvent.click(await screen.findByRole("button", { name: "Import bot" }));
    expect(await screen.findByText("The bot name is too long.")).toBeInTheDocument();
    expect(useChat.getState().builder.open).toBe(true);
  });
});

describe("executive assistant", () => {
  it("is off by default and creates an executive when ticked", async () => {
    saved.mockResolvedValue(bot("new1", { name: "Chief" }));
    render(<NewBot />);
    const box = screen.getByRole("checkbox", { name: "Executive assistant" });
    expect(box).not.toBeChecked();
    fireEvent.change(nameField(), { target: { value: "Chief" } });
    fireEvent.click(box);
    fireEvent.click(screen.getByRole("button", { name: "Create bot" }));
    await waitFor(() => expect(saved).toHaveBeenCalledTimes(1));
    expect(saved.mock.calls[0]![0].config).toMatchObject({ executive: true, setup: true });
  });
});
