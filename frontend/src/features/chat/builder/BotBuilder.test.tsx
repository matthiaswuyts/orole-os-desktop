import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Message } from "@/mygo";

vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", async () => (await import("./testing")).mygoMock);

import { useChat } from "@/stores/chat";
import { useLLM } from "@/stores/llm";
import { BotBuilder } from "./BotBuilder";
import { bot, bridge, config, emit, model, provider, resetBridge, toolInfo } from "./testing";

const saved = bridge.chat.saveBot;

function reply(draftId: string, over: Partial<Message> = {}): Message {
  return { id: "a1", botId: `draft:${draftId}`, seq: 0, role: "assistant", author: "bot:draft", parts: [{ type: "text", text: "Hello there" }], status: "complete", createdAt: 2, ...over };
}

const nameField = () => screen.getByLabelText("Name");
const saveButton = () => screen.getByRole("button", { name: "Save" });
const openTab = (name: string) => fireEvent.click(screen.getByRole("tab", { name }));

beforeEach(() => {
  resetBridge();
  useChat.setState({ bots: [bot("b1", { name: "Travel" }), bot("b2", { name: "Coder" })], selectedBotId: null, builder: { open: true, botId: null } });
  useLLM.setState({
    providers: [provider()], defaults: { chatModel: "openrouter/gpt-x", memoryModel: "" },
    models: { openrouter: { models: [model("gpt-x", { name: "GPT X" })], loading: false, error: null } }, ready: true,
  });
});
afterEach(cleanup);

describe("create and update", () => {
  it("creates a new bot with an empty id, then opens it and closes the builder", async () => {
    saved.mockResolvedValue(bot("new1", { name: "Trip planner" }));
    render(<BotBuilder botId={null} />);
    fireEvent.change(nameField(), { target: { value: "  Trip planner " } });
    fireEvent.click(saveButton());
    await waitFor(() => expect(saved).toHaveBeenCalledTimes(1));
    expect(saved.mock.calls[0]![0]).toMatchObject({ id: "", name: "Trip planner", color: "blue", provider: "", model: "", config: { tools: [], memory: [], limits: { maxSteps: 0, maxToolCalls: 0 }, memoryApproval: false } });
    await waitFor(() => expect(useChat.getState().builder.open).toBe(false));
    expect(useChat.getState().selectedBotId).toBe("new1");
  });

  it("updates an existing bot under its id and stays open", async () => {
    bridge.chat.bot.mockResolvedValue(bot("b1", { name: "Travel", instructions: "Plan trips" }));
    saved.mockResolvedValue(bot("b1", { name: "Trips" }));
    render(<BotBuilder botId="b1" />);
    expect(await screen.findByDisplayValue("Travel")).toBeInTheDocument();
    expect(bridge.chat.bot).toHaveBeenCalledWith("b1");
    expect(saveButton()).toBeDisabled();
    expect(screen.getByText("All changes saved")).toBeInTheDocument();
    fireEvent.change(nameField(), { target: { value: "Trips" } });
    expect(screen.getByText("Unsaved changes")).toBeInTheDocument();
    fireEvent.click(saveButton());
    await waitFor(() => expect(saved).toHaveBeenCalledTimes(1));
    expect(saved.mock.calls[0]![0]).toMatchObject({ id: "b1", name: "Trips", instructions: "Plan trips" });
    await waitFor(() => expect(saveButton()).toBeDisabled());
    expect(screen.getByText("All changes saved")).toBeInTheDocument();
    expect(useChat.getState().builder.open).toBe(true);
  });

  it("shows field problems without calling the daemon", () => {
    render(<BotBuilder botId={null} />);
    expect(screen.queryByText("Give the bot a name.")).toBeNull();
    fireEvent.click(saveButton());
    expect(screen.getByText("Give the bot a name.")).toBeInTheDocument();
    expect(saved).not.toHaveBeenCalled();
  });

  it("limits instructions and shows the counter", () => {
    render(<BotBuilder botId={null} />);
    fireEvent.change(screen.getByRole("textbox", { name: "Instructions" }), { target: { value: "x".repeat(32 * 1024 + 1) } });
    expect(screen.getByText("Instructions are limited to 32,768 characters.")).toBeInTheDocument();
    expect(screen.getByText("32,769 / 32,768")).toBeInTheDocument();
  });

  it("shows the daemon's validation error verbatim and keeps the edits", async () => {
    saved.mockRejectedValue(new Error("bad_request: You can have at most 100 bots. Archive one to add another."));
    render(<BotBuilder botId={null} />);
    fireEvent.change(nameField(), { target: { value: "One too many" } });
    fireEvent.click(saveButton());
    expect(await screen.findByText("You can have at most 100 bots. Archive one to add another.")).toBeInTheDocument();
    expect(nameField()).toHaveValue("One too many");
    expect(useChat.getState().builder.open).toBe(true);
  });

  it("reports a bot that cannot be loaded and offers a retry", async () => {
    bridge.chat.bot.mockRejectedValueOnce(new Error("not_found: bot not found")).mockResolvedValue(bot("b1", { name: "Travel" }));
    render(<BotBuilder botId="b1" />);
    expect(await screen.findByText(/bot not found/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByDisplayValue("Travel")).toBeInTheDocument();
  });

  it("duplicates the form as a new bot named as a copy", async () => {
    bridge.chat.bot.mockResolvedValue(bot("b1", { name: "Travel" }));
    saved.mockResolvedValue(bot("copy1", { name: "Travel (copy)" }));
    render(<BotBuilder botId="b1" />);
    await screen.findByDisplayValue("Travel");
    fireEvent.click(screen.getByRole("button", { name: "More actions" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /Duplicate/ }));
    await waitFor(() => expect(saved).toHaveBeenCalledTimes(1));
    expect(saved.mock.calls[0]![0]).toMatchObject({ id: "", name: "Travel (copy)" });
    await waitFor(() => expect(useChat.getState().selectedBotId).toBe("copy1"));
    expect(useChat.getState().builder.open).toBe(false);
  });
});

describe("persona", () => {
  it("fills the instructions from a template without creating anything", async () => {
    render(<BotBuilder botId={null} />);
    fireEvent.click(screen.getByRole("button", { name: /Start from a template/ }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /Researcher/ }));
    expect((screen.getByRole("textbox", { name: "Instructions" }) as HTMLTextAreaElement).value).toContain("careful research assistant");
    expect(saved).not.toHaveBeenCalled();
  });

  it("asks before a template replaces existing instructions", async () => {
    render(<BotBuilder botId={null} />);
    fireEvent.change(screen.getByRole("textbox", { name: "Instructions" }), { target: { value: "My own text" } });
    fireEvent.click(screen.getByRole("button", { name: /Start from a template/ }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /Writer/ }));
    expect(screen.getByDisplayValue("My own text")).toBeInTheDocument();
    fireEvent.click(await screen.findByRole("button", { name: "Replace instructions" }));
    expect(await screen.findByDisplayValue(/writing partner/)).toBeInTheDocument();
  });
});

describe("model", () => {
  it("shows what 'use default' resolves to and sends provider and model separately", async () => {
    saved.mockResolvedValue(bot("n", { name: "M" }));
    render(<BotBuilder botId={null} />);
    fireEvent.change(nameField(), { target: { value: "M" } });
    openTab("Model");
    expect(await screen.findByText("Default chat model: GPT X.")).toBeInTheDocument();
    fireEvent.keyDown(screen.getByRole("combobox"), { key: "ArrowDown" });
    fireEvent.click(await screen.findByRole("option", { name: /^GPT X/ }));
    fireEvent.click(saveButton());
    await waitFor(() => expect(saved).toHaveBeenCalled());
    expect(saved.mock.calls[0]![0]).toMatchObject({ provider: "openrouter", model: "gpt-x" });
  });

  it("validates limits and sends them as numbers", async () => {
    saved.mockResolvedValue(bot("n", { name: "L" }));
    render(<BotBuilder botId={null} />);
    fireEvent.change(nameField(), { target: { value: "L" } });
    openTab("Model");
    fireEvent.change(await screen.findByLabelText("Max steps"), { target: { value: "31" } });
    fireEvent.click(saveButton());
    expect(await screen.findByText(/Max steps must be a whole number from 1 to 30/)).toBeInTheDocument();
    expect(saved).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText("Max steps"), { target: { value: "12" } });
    fireEvent.change(screen.getByLabelText("Max tool calls"), { target: { value: "40" } });
    fireEvent.click(screen.getByRole("switch", { name: "Hold memory writes for my approval" }));
    fireEvent.click(saveButton());
    await waitFor(() => expect(saved).toHaveBeenCalled());
    expect(saved.mock.calls[0]![0].config).toMatchObject({ limits: { maxSteps: 12, maxToolCalls: 40 }, memoryApproval: true });
  });
});

describe("tools", () => {
  it("lists tools for the chosen model, explains unavailable ones and saves enabled ones", async () => {
    bridge.chat.tools.mockResolvedValue([
      toolInfo("web_search", { name: "Web search", available: false, reason: "This model cannot run hosted web search." }),
      toolInfo("datetime", { name: "Date and time", egress: "Nothing leaves this computer." }),
    ]);
    saved.mockResolvedValue(bot("n", { name: "T" }));
    render(<BotBuilder botId={null} />);
    fireEvent.change(nameField(), { target: { value: "T" } });
    openTab("Tools");
    expect(await screen.findByText(/This model cannot run hosted web search\./)).toBeInTheDocument();
    expect(bridge.chat.tools).toHaveBeenCalledWith({ provider: "", model: "" });
    expect(screen.getByRole("switch", { name: "Web search" })).toHaveAttribute("aria-disabled", "true");
    expect(screen.getByText(/Leaves this computer: Nothing leaves this computer\./)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("switch", { name: "Date and time" }));
    fireEvent.click(saveButton());
    await waitFor(() => expect(saved).toHaveBeenCalled());
    expect(saved.mock.calls[0]![0].config.tools).toEqual([{ id: "datetime" }]);
  });

  it("asks the daemon again when the model changes", async () => {
    render(<BotBuilder botId={null} />);
    openTab("Model");
    fireEvent.keyDown(await screen.findByRole("combobox"), { key: "ArrowDown" });
    fireEvent.click(await screen.findByRole("option", { name: /^GPT X/ }));
    openTab("Tools");
    await waitFor(() => expect(bridge.chat.tools).toHaveBeenLastCalledWith({ provider: "openrouter", model: "gpt-x" }));
  });

  it("validates tool parameters and domains before saving", async () => {
    bridge.chat.tools.mockResolvedValue([toolInfo("web_search", { name: "Web search" })]);
    saved.mockResolvedValue(bot("n", { name: "S" }));
    render(<BotBuilder botId={null} />);
    fireEvent.change(nameField(), { target: { value: "S" } });
    openTab("Tools");
    fireEvent.click(await screen.findByRole("switch", { name: "Web search" }));
    fireEvent.change(screen.getByLabelText("Max results"), { target: { value: "11" } });
    const allowed = screen.getByLabelText("Allowed domains");
    fireEvent.change(allowed, { target: { value: "https://example.com/page" } });
    fireEvent.click(screen.getByRole("button", { name: "Add to Allowed domains" }));
    expect(await screen.findByText(/Enter only the host name/)).toBeInTheDocument();
    fireEvent.change(allowed, { target: { value: "Example.com" } });
    fireEvent.click(screen.getByRole("button", { name: "Add to Allowed domains" }));
    expect(screen.getByRole("button", { name: "Remove example.com" })).toBeInTheDocument();
    fireEvent.click(saveButton());
    expect(await screen.findByText(/Max results must be a whole number from 1 to 10/)).toBeInTheDocument();
    expect(saved).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText("Max results"), { target: { value: "4" } });
    fireEvent.click(saveButton());
    await waitFor(() => expect(saved).toHaveBeenCalled());
    expect(saved.mock.calls[0]![0].config.tools).toEqual([{ id: "web_search", params: { max_results: 4, allowed_domains: ["example.com"] } }]);
  });

  it("offers MCP tools, skills and other bots, and saves the selection", async () => {
    bridge.mcp.list.mockResolvedValue([{ id: "srv", name: "Files", enabled: true }]);
    bridge.mcp.tools.mockResolvedValue([{ name: "read_file", title: "Read file", description: "Reads a file", effect: "read" }]);
    bridge.skills.list.mockResolvedValue([{ name: "pdf-tools", description: "Work with PDFs", enabled: true }]);
    saved.mockResolvedValue(bot("n", { name: "S" }));
    bridge.chat.bot.mockResolvedValue(bot("b1", { name: "Travel" }));
    render(<BotBuilder botId="b1" />);
    await screen.findByDisplayValue("Travel");
    openTab("Tools");
    fireEvent.keyDown(await screen.findByLabelText("Search MCP tools"), { key: "ArrowDown" });
    fireEvent.click(await screen.findByRole("option", { name: /Read file/ }));
    fireEvent.keyDown(await screen.findByLabelText("Search Skills"), { key: "ArrowDown" });
    fireEvent.click(await screen.findByRole("option", { name: /pdf-tools/ }));
    fireEvent.keyDown(await screen.findByLabelText("Search Bots it may ask"), { key: "ArrowDown" });
    const coder = await screen.findByRole("option", { name: /Coder/ });
    expect(screen.queryByRole("option", { name: /Travel/ })).toBeNull();
    fireEvent.click(coder);
    fireEvent.click(screen.getByRole("button", { name: "Save", hidden: true }));
    await waitFor(() => expect(saved).toHaveBeenCalled());
    expect(saved.mock.calls[0]![0].config).toMatchObject({ mcpTools: ["srv/read_file"], skills: ["pdf-tools"], delegateTo: ["b2"] });
  });
});

describe("memory scope access", () => {
  const scopes = [
    { id: "bot:b1", name: "Travel notes", kind: "bot", count: 2, used: 100, limit: 2200 },
    { id: "user", name: "About you", kind: "user", count: 1, used: 10, limit: 1400 },
    { id: "shared:home", name: "Home", kind: "shared", count: 0, used: 0, limit: 2200 },
  ];

  it("keeps grants in the form and saves them with the bot", async () => {
    bridge.chat.bot.mockResolvedValue(bot("b1", { name: "Travel" }));
    bridge.chat.memoryScopes.mockResolvedValue(scopes);
    saved.mockResolvedValue(bot("b1", { name: "Travel" }));
    render(<BotBuilder botId="b1" />);
    await screen.findByDisplayValue("Travel");
    openTab("Memory");
    expect(await screen.findByText("Travel notes")).toBeInTheDocument();
    expect(bridge.chat.memoryScopes).toHaveBeenCalledWith("b1");
    expect(screen.queryByRole("switch", { name: "Read Travel notes" })).toBeNull();
    fireEvent.click(screen.getByRole("switch", { name: "Write About you" }));
    expect(screen.getByRole("switch", { name: "Read About you" })).toHaveAttribute("aria-checked", "true");
    fireEvent.click(screen.getByRole("switch", { name: "Read Home" }));
    fireEvent.click(saveButton());
    await waitFor(() => expect(saved).toHaveBeenCalled());
    expect(saved.mock.calls[0]![0].config.memory).toEqual([{ scope: "user", read: true, write: true }, { scope: "shared:home", read: true, write: false }]);
  });

  it("asks to save a new bot first", () => {
    render(<BotBuilder botId={null} />);
    openTab("Memory");
    expect(within(screen.getByRole("tabpanel", { name: "Memory" })).getByText(/Save the bot first/)).toBeInTheDocument();
    expect(bridge.chat.memoryScopes).not.toHaveBeenCalled();
  });
});

describe("unsaved changes", () => {
  it("closes at once when nothing changed", () => {
    render(<BotBuilder botId={null} />);
    fireEvent.click(screen.getByRole("button", { name: "Close the bot builder" }));
    expect(useChat.getState().builder.open).toBe(false);
  });

  it("asks before closing over edits, and keeps them if you stay", async () => {
    render(<BotBuilder botId={null} />);
    fireEvent.change(nameField(), { target: { value: "Draft" } });
    fireEvent.click(screen.getByRole("button", { name: "Close the bot builder" }));
    expect(await screen.findByText("Discard unsaved changes?")).toBeInTheDocument();
    expect(useChat.getState().builder.open).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByText("Discard unsaved changes?")).toBeNull());
    expect(nameField()).toHaveValue("Draft");
    fireEvent.click(screen.getByRole("button", { name: "Close the bot builder" }));
    fireEvent.click(await screen.findByRole("button", { name: "Discard and close" }));
    await waitFor(() => expect(useChat.getState().builder.open).toBe(false));
  });

  it("discard returns the form to the last saved state", async () => {
    bridge.chat.bot.mockResolvedValue(bot("b1", { name: "Travel" }));
    render(<BotBuilder botId="b1" />);
    await screen.findByDisplayValue("Travel");
    fireEvent.change(nameField(), { target: { value: "Changed" } });
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    fireEvent.click(await screen.findByRole("button", { name: "Discard changes" }));
    await waitFor(() => expect(nameField()).toHaveValue("Travel"));
  });
});

describe("import, export and archive", () => {
  const file = (content: unknown) => new File([typeof content === "string" ? content : JSON.stringify(content)], "bot.json", { type: "application/json" });
  const chooseFile = async (f: File) => {
    fireEvent.click(screen.getByRole("button", { name: "More actions" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /Import from file/ }));
    fireEvent.change(screen.getByLabelText("Choose a bot export file"), { target: { files: [f] } });
  };

  it("refuses an export of another version and says why", async () => {
    render(<BotBuilder botId={null} />);
    await chooseFile(file({ version: 2, bot: { name: "Future" } }));
    expect(await screen.findByText(/Export version 2 is not supported; only version 1 can be imported\./)).toBeInTheDocument();
    expect(bridge.chat.importBot).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "Import bot" })).toBeNull();
  });

  it("refuses text that is not JSON", async () => {
    render(<BotBuilder botId={null} />);
    await chooseFile(file("hello"));
    expect(await screen.findByText("This is not valid JSON.")).toBeInTheDocument();
  });

  it("imports a version 1 export as a new bot and opens it", async () => {
    bridge.chat.importBot.mockResolvedValue(bot("imp", { name: "Imported" }));
    render(<BotBuilder botId={null} />);
    await chooseFile(file({ version: 1, bot: { name: "Imported", instructions: "Be brief" } }));
    fireEvent.click(await screen.findByRole("button", { name: "Import bot" }));
    await waitFor(() => expect(bridge.chat.importBot).toHaveBeenCalledTimes(1));
    expect(bridge.chat.importBot.mock.calls[0]![0]).toMatchObject({ version: 1, bot: { id: "", name: "Imported", instructions: "Be brief" } });
    await waitFor(() => expect(useChat.getState().selectedBotId).toBe("imp"));
    expect(useChat.getState().builder.open).toBe(false);
  });

  it("shows the daemon's rejection of an import", async () => {
    bridge.chat.importBot.mockRejectedValue(new Error("bad_request: The bot name is too long."));
    render(<BotBuilder botId={null} />);
    await chooseFile(file({ version: 1, bot: { name: "Imported" } }));
    fireEvent.click(await screen.findByRole("button", { name: "Import bot" }));
    expect(await screen.findByText("The bot name is too long.")).toBeInTheDocument();
    expect(useChat.getState().builder.open).toBe(true);
  });

  it("archives only after confirmation", async () => {
    bridge.chat.bot.mockResolvedValue(bot("b1", { name: "Travel" }));
    bridge.chat.archiveBot.mockResolvedValue(undefined);
    render(<BotBuilder botId="b1" />);
    await screen.findByDisplayValue("Travel");
    fireEvent.click(screen.getByRole("button", { name: "More actions" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /Archive/ }));
    fireEvent.click(await screen.findByRole("button", { name: "Cancel" }));
    expect(bridge.chat.archiveBot).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "More actions" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /Archive/ }));
    fireEvent.click(await screen.findByRole("button", { name: "Archive bot" }));
    await waitFor(() => expect(bridge.chat.archiveBot).toHaveBeenCalledWith("b1"));
    await waitFor(() => expect(useChat.getState().builder.open).toBe(false));
  });
});

describe("preview", () => {
  const send = (text: string) => {
    fireEvent.change(screen.getByLabelText("Preview message"), { target: { value: text } });
    fireEvent.click(screen.getByRole("button", { name: "Send preview message" }));
  };
  const userMessage = (draftId: string, text: string): Message => ({ id: `u-${text}`, botId: `draft:${draftId}`, seq: 0, role: "user", author: "user", parts: [{ type: "text", text }], status: "complete", createdAt: 1 });

  it("is labelled as unsaved and explains why it is disabled until the form is valid", async () => {
    render(<BotBuilder botId={null} />);
    expect(screen.getByText("Preview - not saved, memory disabled")).toBeInTheDocument();
    expect(await screen.findByText(/Fix the problem in the form to preview the bot\./)).toBeInTheDocument();
    expect(screen.getByLabelText("Preview message")).toBeDisabled();
    fireEvent.change(nameField(), { target: { value: "Previewer" } });
    await waitFor(() => expect(screen.getByLabelText("Preview message")).toBeEnabled());
  });

  it("is disabled with an explanation when no provider is connected", async () => {
    bridge.llm.providers.mockResolvedValue([provider({ connected: false })]);
    useLLM.setState({ providers: [provider({ connected: false })], ready: true });
    render(<BotBuilder botId={null} />);
    fireEvent.change(nameField(), { target: { value: "Previewer" } });
    expect(await screen.findByText(/No provider is connected\. Connect one to try this bot\./)).toBeInTheDocument();
    expect(screen.getByLabelText("Preview message")).toBeDisabled();
  });

  it("runs the unsaved draft and renders only this draft's messages", async () => {
    let draftId = "";
    bridge.chat.draftSend.mockImplementation(async (req: { draftId: string; text: string }) => { draftId = req.draftId; return userMessage(req.draftId, req.text); });
    render(<BotBuilder botId={null} />);
    fireEvent.change(nameField(), { target: { value: "Previewer" } });
    fireEvent.change(screen.getByRole("textbox", { name: "Instructions" }), { target: { value: "Answer in one word." } });
    await waitFor(() => expect(screen.getByLabelText("Preview message")).toBeEnabled());
    send("Hi bot");
    await waitFor(() => expect(bridge.chat.draftSend).toHaveBeenCalledTimes(1));
    const request = bridge.chat.draftSend.mock.calls[0]![0];
    expect(request).toMatchObject({ bot: { id: "", name: "Previewer", instructions: "Answer in one word.", config: config({ limits: { maxSteps: 0, maxToolCalls: 0 } }) }, history: [], text: "Hi bot" });
    expect(request.draftId).toMatch(/^[A-Za-z0-9_-]{1,64}$/);
    expect(saved).not.toHaveBeenCalled();
    expect(await screen.findByText("Hi bot")).toBeInTheDocument();

    emit("chatMessage", reply("someone-else", { id: "x1", parts: [{ type: "text", text: "Wrong draft" }] }));
    emit("chatMessage", { ...reply(draftId), botId: "b1", id: "x2", parts: [{ type: "text", text: "A real conversation" }] });
    emit("chatMessage", reply(draftId, { status: "running", parts: [{ type: "text", text: "Hel" }] }));
    expect(await screen.findByText("Hel")).toBeInTheDocument();
    emit("chatMessage", reply(draftId));
    expect(await screen.findByText("Hello there")).toBeInTheDocument();
    expect(screen.queryByText("Hel")).toBeNull();
    expect(screen.queryByText("Wrong draft")).toBeNull();
    expect(screen.queryByText("A real conversation")).toBeNull();
  });

  it("sends earlier completed turns as history and starts over on reset", async () => {
    let draftId = "";
    bridge.chat.draftSend.mockImplementation(async (req: { draftId: string; text: string }) => { draftId = req.draftId; return userMessage(req.draftId, req.text); });
    render(<BotBuilder botId={null} />);
    fireEvent.change(nameField(), { target: { value: "Previewer" } });
    await waitFor(() => expect(screen.getByLabelText("Preview message")).toBeEnabled());
    send("First");
    await screen.findByText("First");
    const firstId = draftId;
    emit("chatMessage", reply(draftId));
    await screen.findByText("Hello there");
    send("Second");
    await waitFor(() => expect(bridge.chat.draftSend).toHaveBeenCalledTimes(2));
    expect(bridge.chat.draftSend.mock.calls[1]![0]).toMatchObject({ draftId: firstId, history: [{ role: "user", text: "First" }, { role: "assistant", text: "Hello there" }], text: "Second" });

    fireEvent.click(screen.getByRole("button", { name: "Reset preview" }));
    await waitFor(() => expect(screen.queryByText("First")).toBeNull());
    expect(screen.queryByText("Hello there")).toBeNull();
    emit("chatMessage", reply(firstId, { id: "late", parts: [{ type: "text", text: "Late reply" }] }));
    expect(screen.queryByText("Late reply")).toBeNull();
    send("Third");
    await waitFor(() => expect(bridge.chat.draftSend).toHaveBeenCalledTimes(3));
    const third = bridge.chat.draftSend.mock.calls[2]![0];
    expect(third.draftId).not.toBe(firstId);
    expect(third.history).toEqual([]);
  });

  it("shows the daemon's refusal and keeps the typed message", async () => {
    bridge.chat.draftSend.mockRejectedValue(new Error("bad_request: The preview is still replying."));
    render(<BotBuilder botId={null} />);
    fireEvent.change(nameField(), { target: { value: "Previewer" } });
    await waitFor(() => expect(screen.getByLabelText("Preview message")).toBeEnabled());
    send("Again");
    expect(await screen.findByText("The preview is still replying.")).toBeInTheDocument();
    expect(screen.getByLabelText("Preview message")).toHaveValue("Again");
  });
});
