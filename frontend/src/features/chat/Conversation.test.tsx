import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Bot, BotChange, Message, RunState, SendRequest } from "@/mygo";

const bridge = vi.hoisted(() => ({
  messages: vi.fn(), send: vi.fn(), cancel: vi.fn(), regenerate: vi.fn(), clear: vi.fn(), exportBot: vi.fn(),
  begin: vi.fn(), chunk: vi.fn(), commit: vi.fn(), discard: vi.fn(),
  run: null as ((s: RunState) => void) | null,
  message: null as ((m: Message) => void) | null,
}));

vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({
  Chat: {
    messages: bridge.messages, send: bridge.send, cancel: bridge.cancel, regenerate: bridge.regenerate, clear: bridge.clear, exportBot: bridge.exportBot,
    beginAttachment: bridge.begin, attachmentChunk: bridge.chunk, commitAttachment: bridge.commit, discardAttachment: bridge.discard,
  },
  LLM: { models: vi.fn() },
  Daemon: { health: vi.fn() },
  Shell: { openExternal: vi.fn() },
  events: {
    chatRun: { on: (f: (s: RunState) => void) => { bridge.run = f; return () => { bridge.run = null; }; } },
    chatMessage: { on: (f: (m: Message) => void) => { bridge.message = f; return () => { bridge.message = null; }; } },
    chatBot: { on: (_f: (c: BotChange) => void) => () => {} },
  },
}));

import { Conversation } from "@/features/chat/Conversation";
import { useChat } from "@/stores/chat";
import { useChatMessages } from "@/stores/chatMessages";
import { useDaemon } from "@/stores/daemon";
import { useLLM } from "@/stores/llm";

const bot = (over: Partial<Bot> = {}): Bot => ({
  id: "a", name: "Atlas", avatar: "", color: "blue", description: "Plans trips", instructions: "", provider: "openrouter", model: "claude", reasoning: "",
  config: { tools: [], mcpTools: [], skills: [], delegateTo: [], memory: [], limits: { maxSteps: 0, maxToolCalls: 0 }, memoryApproval: false },
  createdAt: 1, updatedAt: 1, running: false, ...over,
});
const withTools = (...ids: string[]) => bot({ config: { ...bot().config, tools: ids.map((id) => ({ id })) } });
const msg = (seq: number, over: Partial<Message> = {}): Message => ({
  id: `m${seq}`, botId: "a", seq, role: seq % 2 ? "user" : "assistant", author: seq % 2 ? "user" : "bot:a",
  parts: [{ type: "text", text: `${seq % 2 ? "question" : "answer"} ${seq}` }], status: "complete", createdAt: seq * 10, ...over,
});
const caps = { tools: true, vision: false, pdf: true, reasoning: false, hostedTools: [], deepResearch: false, contextWindow: 0 };

let stubs: Array<() => void> = [];
beforeEach(() => {
  Object.values(bridge).forEach((v) => { if (typeof v === "function" && "mockReset" in v) v.mockReset(); });
  stubs.push(() => vi.unstubAllGlobals());
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  useDaemon.setState({ link: "connected" });
  useChat.setState({ bots: [bot()], selectedBotId: "a", running: {}, builder: { open: false, botId: null }, available: true, ready: true, loading: false, error: null });
  useChatMessages.setState({ byBot: {}, runs: {}, drafts: {}, problems: {} });
  Element.prototype.scrollTo = () => {};
  useLLM.setState({
    providers: [{ id: "openrouter", name: "OpenRouter", kind: "builtin", connected: true }], ready: true, defaults: { chatModel: "openrouter/claude", memoryModel: "" },
    models: { openrouter: { models: [{ id: "claude", name: "Claude Sonnet", caps, reasoning: [] }], loading: false, error: null } },
  });
  bridge.messages.mockResolvedValue({ messages: [], hasMore: false });
  bridge.cancel.mockResolvedValue(undefined);
  bridge.regenerate.mockResolvedValue(undefined);
});
afterEach(() => { cleanup(); stubs.forEach((f) => f()); stubs = []; });

const open = async (messages: Message[] = [], b: Bot = bot(), hasMore = false) => {
  bridge.messages.mockResolvedValue({ messages, hasMore });
  const view = render(<Conversation key={b.id} bot={b} />);
  if (messages.length) await screen.findAllByText(/answer|question/);
  else await screen.findByText(/Say hello/);
  return view;
};
const box = () => screen.getByRole("textbox", { name: "Message Atlas" }) as HTMLTextAreaElement;
const type = (text: string) => fireEvent.change(box(), { target: { value: text } });

describe("conversation rendering", () => {
  it("shows the bot, its description and a plain prompt when there are no messages", async () => {
    await open();
    const empty = document.querySelector("[data-slot=conversation-empty]") as HTMLElement;
    expect(within(empty).getByText("Atlas")).toBeInTheDocument();
    expect(within(empty).getByText("Plans trips")).toBeInTheDocument();
    expect(within(empty).getByText("Say hello to start the conversation.")).toBeInTheDocument();
  });

  it("renders the stored conversation with accessible message labels", async () => {
    await open([msg(1), msg(2)]);
    expect(screen.getByRole("region", { name: "Conversation with Atlas" })).toBeInTheDocument();
    expect(screen.getByText("question 1")).toBeInTheDocument();
    expect(screen.getByText("answer 2")).toBeInTheDocument();
    expect(screen.getByText("You said:")).toBeInTheDocument();
    expect(screen.getByText("Atlas said:")).toBeInTheDocument();
  });

  it("dims a queued user message and labels it", async () => {
    await open([msg(1), msg(2), msg(3, { status: "pending" })]);
    const queued = screen.getByText("question 3").closest("[data-slot=chat-user]")!;
    expect(queued).toHaveAttribute("data-queued", "true");
    expect(within(queued as HTMLElement).getByText("Queued")).toBeInTheDocument();
    expect(screen.getByText("question 1").closest("[data-slot=chat-user]")).not.toHaveAttribute("data-queued");
  });

  it("replaces a streaming reply in place as snapshots arrive", async () => {
    await open([msg(1), msg(2, { status: "running", parts: [{ type: "text", text: "Hel" }] })]);
    act(() => bridge.message!(msg(2, { status: "running", parts: [{ type: "text", text: "Hello wor" }] })));
    expect(await screen.findByText("Hello wor")).toBeInTheDocument();
    act(() => bridge.message!(msg(2, { status: "complete", parts: [{ type: "text", text: "Hello world" }] })));
    expect(await screen.findByText("Hello world")).toBeInTheDocument();
    expect(screen.queryByText("Hello wor")).toBeNull();
    expect(screen.getAllByText(/Atlas said:/)).toHaveLength(1);
  });

  it("announces Responding and Done through a polite live region", async () => {
    await open([msg(1), msg(2)]);
    const live = screen.getAllByRole("status").find((el) => el.getAttribute("aria-live") === "polite")!;
    expect(live).toHaveTextContent("");
    act(() => bridge.run!({ botId: "a", running: true }));
    await waitFor(() => expect(live).toHaveTextContent("Responding"));
    act(() => bridge.run!({ botId: "a", running: false }));
    await waitFor(() => expect(live).toHaveTextContent("Done"));
  });

  it("offers Load earlier messages when older history exists and pages by the first seq", async () => {
    await open([msg(3), msg(4)], bot(), true);
    bridge.messages.mockResolvedValueOnce({ messages: [msg(1), msg(2)], hasMore: false });
    fireEvent.click(screen.getByRole("button", { name: "Load earlier messages" }));
    expect(await screen.findByText("question 1")).toBeInTheDocument();
    expect(bridge.messages).toHaveBeenLastCalledWith({ botId: "a", beforeSeq: 3, limit: 50 });
    expect(screen.queryByRole("button", { name: "Load earlier messages" })).toBeNull();
  });

  it("shows a load failure with Retry", async () => {
    bridge.messages.mockRejectedValueOnce(new Error("internal: daemon down"));
    render(<Conversation bot={bot()} />);
    expect(await screen.findByText(/Could not load this conversation: daemon down/)).toBeInTheDocument();
    bridge.messages.mockResolvedValueOnce({ messages: [msg(1), msg(2)], hasMore: false });
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByText("answer 2")).toBeInTheDocument();
  });
});

describe("message actions", () => {
  it("offers Regenerate only on the last reply and wires it to the daemon", async () => {
    await open([msg(1), msg(2), msg(3), msg(4)]);
    const regenerate = screen.getAllByRole("button", { name: "Regenerate response" });
    expect(regenerate).toHaveLength(1);
    fireEvent.click(regenerate[0]!);
    await waitFor(() => expect(bridge.regenerate).toHaveBeenCalledWith({ botId: "a", messageId: "m4" }));
  });

  it("shows a failed reply with its error and a Retry that regenerates it", async () => {
    await open([msg(1), msg(2, { status: "error", error: "Invalid API key", parts: [] })]);
    expect(screen.getByRole("alert")).toHaveTextContent("Invalid API key");
    fireEvent.click(within(screen.getByRole("alert")).getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(bridge.regenerate).toHaveBeenCalledWith({ botId: "a", messageId: "m2" }));
  });

  it("hides actions on a reply that is still streaming", async () => {
    await open([msg(1), msg(2, { status: "running", parts: [{ type: "text", text: "so far" }] })]);
    expect(screen.queryByRole("button", { name: "Copy response" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Regenerate response" })).toBeNull();
  });
});

describe("composer", () => {
  it("sends with Enter, keeps a newline for Shift+Enter, and clears afterwards", async () => {
    bridge.send.mockImplementation(async (r: SendRequest) => msg(3, { parts: [{ type: "text", text: r.text }] }));
    await open([msg(1), msg(2)]);
    const send = screen.getByRole("button", { name: "Send message" });
    expect(send).toBeDisabled();
    type("hello");
    expect(send).toBeEnabled();
    fireEvent.keyDown(box(), { key: "Enter", shiftKey: true });
    expect(bridge.send).not.toHaveBeenCalled();
    fireEvent.keyDown(box(), { key: "Enter" });
    await waitFor(() => expect(bridge.send).toHaveBeenCalledTimes(1));
    expect(bridge.send.mock.calls[0]![0]).toMatchObject({ botId: "a", text: "hello", attachmentIds: [], mode: "" });
    expect(bridge.send.mock.calls[0]![0].clientNonce).toMatch(/\S/);
    await waitFor(() => expect(box()).toHaveValue(""));
    expect(await screen.findByText("hello")).toBeInTheDocument();
    expect(useChatMessages.getState().drafts.a?.text).toBe("");
  });

  it("does not send whitespace and stays disabled with nothing to send", async () => {
    await open([msg(1), msg(2)]);
    type("   ");
    fireEvent.keyDown(box(), { key: "Enter" });
    expect(bridge.send).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Send message" })).toBeDisabled();
  });

  it("does not submit while an IME composition is active", async () => {
    await open([msg(1), msg(2)]);
    type("こんにちは");
    fireEvent.keyDown(box(), { key: "Enter", isComposing: true });
    expect(bridge.send).not.toHaveBeenCalled();
  });

  it("lets a message be queued while a reply runs and offers Stop", async () => {
    bridge.send.mockResolvedValue(msg(3, { status: "pending" }));
    await open([msg(1), msg(2, { status: "running", parts: [{ type: "text", text: "thinking" }] })]);
    act(() => bridge.run!({ botId: "a", running: true }));
    type("one more thing");
    fireEvent.click(await screen.findByRole("button", { name: "Queue message" }));
    await waitFor(() => expect(bridge.send).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole("button", { name: "Stop" }));
    await waitFor(() => expect(bridge.cancel).toHaveBeenCalledWith("a"));
    expect(await screen.findByText("Queued")).toBeInTheDocument();
  });

  it("keeps each bot's draft while another is open", async () => {
    const first = await open([msg(1), msg(2)]);
    type("half-written");
    first.unmount();
    useChat.setState({ bots: [bot(), bot({ id: "b", name: "Beta" })] });
    bridge.messages.mockResolvedValue({ messages: [], hasMore: false });
    const other = render(<Conversation key="b" bot={bot({ id: "b", name: "Beta" })} />);
    await screen.findByText(/Say hello/);
    expect(screen.getByRole("textbox", { name: "Message Beta" })).toHaveValue("");
    other.unmount();
    render(<Conversation key="a" bot={bot()} />);
    await waitFor(() => expect(box()).toHaveValue("half-written"));
  });

  it("offers Research mode only when the research tool is on, sends mode research once and shows a pill", async () => {
    bridge.send.mockImplementation(async (r: SendRequest) => msg(3, { parts: [{ type: "text", text: r.text }] }));
    const view = await open([msg(1), msg(2)], withTools("web_search"));
    fireEvent.click(screen.getByRole("button", { name: "Add files, research and tools" }));
    expect(screen.queryByRole("menuitemcheckbox", { name: "Research mode" })).toBeNull();
    expect(screen.getByRole("menuitem", { name: "Web search" })).toHaveAttribute("aria-disabled", "true");
    view.unmount();
    cleanup();

    await open([msg(1), msg(2)], withTools("research"));
    fireEvent.click(screen.getByRole("button", { name: "Add files, research and tools" }));
    fireEvent.click(await screen.findByRole("menuitemcheckbox", { name: "Research mode" }));
    expect(await screen.findByRole("button", { name: /Research mode on/ })).toBeInTheDocument();
    type("why is the sky blue");
    fireEvent.keyDown(box(), { key: "Enter" });
    await waitFor(() => expect(bridge.send).toHaveBeenCalledTimes(1));
    expect(bridge.send.mock.calls[0]![0]).toMatchObject({ mode: "research", text: "why is the sky blue" });
    await waitFor(() => expect(screen.queryByRole("button", { name: /Research mode on/ })).toBeNull());
  });

  it("links Edit bot from the + menu to the builder", async () => {
    await open([msg(1), msg(2)]);
    fireEvent.click(screen.getByRole("button", { name: "Add files, research and tools" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Edit bot" }));
    expect(useChat.getState().builder).toEqual({ open: true, botId: "a" });
  });

  describe("attachments", () => {
    const pick = async (...files: File[]) => {
      const click = vi.spyOn(HTMLInputElement.prototype, "click").mockImplementation(() => {});
      fireEvent.click(screen.getByRole("button", { name: "Add files, research and tools" }));
      fireEvent.click(await screen.findByRole("menuitem", { name: "Attach files" }));
      await waitFor(() => expect(click).toHaveBeenCalled());
      const input = click.mock.contexts[0] as HTMLInputElement;
      Object.defineProperty(input, "files", { value: files });
      act(() => { input.onchange?.(new Event("change")); });
      click.mockRestore();
    };
    beforeEach(() => {
      bridge.begin.mockResolvedValue({ id: "up1" });
      bridge.chunk.mockResolvedValue(undefined);
      bridge.commit.mockResolvedValue({ id: "att1", name: "notes.txt", mime: "text/plain", size: 5 });
      bridge.discard.mockResolvedValue(undefined);
    });

    it("stages the file, shows it as done, sends its id, and removes it from the composer", async () => {
      bridge.send.mockResolvedValue(msg(3));
      await open([msg(1), msg(2)]);
      await pick(new File(["hello"], "notes.txt", { type: "text/plain" }));
      const chip = await screen.findByText("notes.txt");
      await waitFor(() => expect(chip.closest("[data-slot=composer-attachment]")).toHaveAttribute("data-state", "done"));
      expect(bridge.chunk).toHaveBeenCalledWith({ id: "up1", seq: 0, dataBase64: btoa("hello") });
      expect(screen.getByRole("button", { name: "Send message" })).toBeEnabled();
      fireEvent.click(screen.getByRole("button", { name: "Send message" }));
      await waitFor(() => expect(bridge.send).toHaveBeenCalled());
      expect(bridge.send.mock.calls[0]![0]).toMatchObject({ text: "", attachmentIds: ["att1"] });
      await waitFor(() => expect(screen.queryByText("notes.txt")).toBeNull());
    });

    it("shows an error chip for a file the daemon rejects, blocks sending, and discards on remove", async () => {
      bridge.commit.mockRejectedValue(new Error("bad_request: not a supported file type"));
      await open([msg(1), msg(2)]);
      type("see attached");
      await pick(new File(["x"], "weird.bin", { type: "" }));
      const chip = (await screen.findByText("weird.bin")).closest("[data-slot=composer-attachment]") as HTMLElement;
      await waitFor(() => expect(chip).toHaveAttribute("data-state", "error"));
      expect(within(chip).getByRole("alert")).toHaveTextContent("not a supported file type");
      expect(screen.getByRole("button", { name: "Send message" })).toBeDisabled();
      fireEvent.click(within(chip).getByRole("button", { name: "Remove weird.bin" }));
      await waitFor(() => expect(screen.queryByText("weird.bin")).toBeNull());
      expect(screen.getByRole("button", { name: "Send message" })).toBeEnabled();
    });

    it("explains up front when the model cannot read images", async () => {
      await open([msg(1), msg(2)]);
      await pick(new File(["png"], "shot.png", { type: "image/png" }));
      const chip = (await screen.findByText("shot.png")).closest("[data-slot=composer-attachment]") as HTMLElement;
      expect(within(chip).getByRole("alert")).toHaveTextContent("cannot read images");
      expect(bridge.begin).not.toHaveBeenCalled();
    });
  });
});

describe("header", () => {
  it("shows the bot, its model and its tools", async () => {
    await open([msg(1), msg(2)], withTools("web_search", "datetime"));
    const header = document.querySelector("[data-slot=conversation-header]") as HTMLElement;
    expect(within(header).getByRole("heading", { name: "Atlas" })).toBeInTheDocument();
    expect(within(header).getByText("Claude Sonnet")).toBeInTheDocument();
    expect(within(header).getByText("Web search")).toBeInTheDocument();
    expect(within(header).getByText("Date lookup")).toBeInTheDocument();
  });

  it("marks the default model as such", async () => {
    await open([msg(1), msg(2)], bot({ provider: "", model: "" }));
    expect(screen.getAllByText("Claude Sonnet (default)").length).toBeGreaterThan(0);
  });

  it("clears the conversation only after confirmation", async () => {
    bridge.clear.mockResolvedValue(undefined);
    await open([msg(1), msg(2)]);
    fireEvent.click(screen.getByRole("button", { name: "More actions for Atlas" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /Clear conversation/ }));
    expect(bridge.clear).not.toHaveBeenCalled();
    fireEvent.click(await screen.findByRole("button", { name: "Clear conversation" }));
    await waitFor(() => expect(bridge.clear).toHaveBeenCalledWith("a"));
    await waitFor(() => expect(screen.getByText(/Say hello/)).toBeInTheDocument());
  });

  it("opens the builder and exports the bot as a download", async () => {
    bridge.exportBot.mockResolvedValue({ version: 1, bot: { name: "Atlas" } });
    const create = vi.fn(() => "blob:x");
    vi.stubGlobal("URL", Object.assign(URL, { createObjectURL: create, revokeObjectURL: vi.fn() }));
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    await open([msg(1), msg(2)]);
    fireEvent.click(screen.getByRole("button", { name: "More actions for Atlas" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Export bot" }));
    await waitFor(() => expect(bridge.exportBot).toHaveBeenCalledWith("a"));
    await waitFor(() => expect(click).toHaveBeenCalled());
    click.mockRestore();
    fireEvent.click(screen.getByRole("button", { name: "More actions for Atlas" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Edit bot" }));
    expect(useChat.getState().builder).toEqual({ open: true, botId: "a" });
  });
});
