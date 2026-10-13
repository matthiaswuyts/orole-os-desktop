import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Message } from "@/mygo";
import { useChat } from "@/stores/chat";

vi.mock("@/lib/openExternal", () => ({ isWebURL: (u: string) => /^https?:/.test(u), openExternal: vi.fn() }));

import { ChatMessageView } from "./MessageBubble";

const message = (parts: Message["parts"], over: Partial<Message> = {}): Message => ({
  id: "m", botId: "a", seq: 2, role: "assistant", author: "bot:a", parts, status: "complete", createdAt: 1, ...over,
});
const view = (m: Message, onRetry?: () => void) => render(<ChatMessageView message={m} botName="Atlas" botAvatar="" onRetry={onRetry} />);
afterEach(cleanup);

describe("message parts", () => {
  it("renders markdown and hides the work behind a collapsed timeline, without the model's reasoning", () => {
    view(message([
      { type: "reasoning", text: "weighing options", durationMs: 4200 },
      { type: "tool", id: "t", name: "web_search", status: "done", durationMs: 1200, summary: "oslo flights" },
      { type: "text", text: "**Bold** answer" },
    ]));
    expect(screen.queryByText(/Thought|Thinking/)).toBeNull();
    expect(screen.queryByText("weighing options")).toBeNull();
    expect(screen.getByText("Bold").tagName).toBe("STRONG");
    expect(screen.queryByText("Searched the web")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "1 step" }));
    expect(screen.getByText("Searched the web")).toBeInTheDocument();
    expect(screen.getAllByText("oslo flights").length).toBeGreaterThan(0);
  });

  it("gathers citations into one sources popover and lists only http(s) links as links", () => {
    view(message([
      { type: "text", text: "see" },
      { type: "citation", url: "https://a.test/x", title: "A page" },
      { type: "citation", url: "https://a.test/x", title: "dup" },
      { type: "citation", url: "javascript:alert(1)", title: "bad" },
    ]));
    fireEvent.click(screen.getByRole("button", { name: /2 sources/ }));
    const list = screen.getByRole("list");
    expect(within(list).getByRole("link", { name: /A page/ })).toHaveAttribute("href", "https://a.test/x");
    expect(within(list).queryByRole("link", { name: /bad/ })).toBeNull();
  });

  it("shows a research report with its title and a copy action", () => {
    view(message([{ type: "report", title: "Sky colour", text: "Rayleigh scattering." }]));
    const report = screen.getByRole("region", { name: "Sky colour" });
    expect(within(report).getByText("Rayleigh scattering.")).toBeInTheDocument();
    expect(within(report).getByRole("button", { name: "Copy report" })).toBeEnabled();
  });

  it("shows memory as chips and notices inline, and a delegation behind the timeline", () => {
    view(message([
      { type: "delegation", name: "Coder", status: "done" },
      { type: "memory", text: "likes tea" },
      { type: "notice", text: "Earlier messages were summarised." },
    ]));
    expect(screen.queryByText(/Handoff by/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "1 step" }));
    expect(screen.getByText("Handoff by Atlas")).toBeInTheDocument();
    expect(screen.getAllByText("Coder").length).toBeGreaterThan(0);
    expect(screen.getByText("remembered 1")).toBeInTheDocument();
    expect(screen.getByText("likes tea")).toBeInTheDocument();
    expect(screen.getByRole("note")).toHaveTextContent("Earlier messages were summarised.");
  });

  it("marks a tool left running in a settled message as not finished", () => {
    view(message([{ type: "tool", id: "t", name: "web_fetch", status: "running" }]));
    fireEvent.click(screen.getByRole("button", { name: "1 step" }));
    expect(screen.getByText("Page read did not finish")).toBeInTheDocument();
  });

  it("turns research progress rows into an outline and keeps the sources out of the answer", () => {
    view(message([
      { type: "tool", id: "research:plan", name: "research", status: "done", summary: "2 questions" },
      { type: "tool", id: "research:q1", name: "research", status: "done", summary: "What is X? (3 sources)" },
      { type: "tool", id: "research:q2", name: "research", status: "error", summary: "What is Y?" },
      { type: "tool", id: "research:sources", name: "research", status: "done", summary: "3 sources read" },
      { type: "tool", id: "research:synthesis", name: "research", status: "done", summary: "Report ready" },
      { type: "report", title: "X and Y", text: "## Summary\n\nX is a thing [1]." },
      { type: "citation", url: "https://a.test/x", title: "A page" },
    ]));
    expect(screen.queryByText("What is X?")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Researched 3 sources" }));
    const outline = screen.getByText("Research").closest("[data-slot=research-report]") as HTMLElement;
    expect(within(outline).getByText("What is X?")).toBeInTheDocument();
    expect(within(outline).getByText("3 src")).toBeInTheDocument();
    expect(within(outline).getByText("Could not be researched")).toBeInTheDocument();
    expect(within(outline).getByText("2/3 sections · 3 sources read")).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "X and Y" })).toHaveTextContent("X is a thing");
  });

  it("shows a handoff to another bot in the timeline with a link to its chat, and hides the raw ask_bot row", () => {
    const open = vi.fn();
    render(<ChatMessageView message={message([
      { type: "tool", id: "t", name: "ask_bot", status: "done", summary: "Answer from Researcher" },
      { type: "delegation", id: "d", name: "Researcher", botId: "r1", status: "done", text: "Research local agent computers", summary: "Use Incus per agent." },
    ])} botName="Executive" botAvatar="" bots={[{ id: "r1", name: "Researcher", avatar: "" }]} onOpenBot={open} />);
    fireEvent.click(screen.getByRole("button", { name: "1 step" }));
    const card = document.querySelector("[data-slot=agent-handoff]") as HTMLElement;
    expect(within(card).getByText("Executive")).toBeInTheDocument();
    expect(within(card).getByText("Researcher")).toBeInTheDocument();
    expect(screen.getByText("Research local agent computers")).toBeInTheDocument();
    expect(screen.getByText(/Researcher answered:/)).toBeInTheDocument();
    expect(screen.getByText("Use Incus per agent.")).toBeInTheDocument();
    expect(screen.queryByText(/Ask bot/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Open Researcher's chat" }));
    expect(open).toHaveBeenCalledWith("r1");
  });

  it("draws a task another bot handed off as a handoff card naming who handed it off", () => {
    render(<ChatMessageView message={message([{ type: "text", text: "Compare Incus and Docker for agent sandboxes" }], { role: "user", author: "bot:exec" })}
      botName="Researcher" botAvatar="" bots={[{ id: "exec", name: "Executive", avatar: "" }]} handoffSettled={false} />);
    const card = screen.getByText("Handoff by Executive", { selector: "p" }).closest("[data-slot=agent-handoff]") as HTMLElement;
    expect(within(card).getByText("Executive")).toBeInTheDocument();
    expect(within(card).getByText("Researcher")).toBeInTheDocument();
    expect(screen.getByText("Compare Incus and Docker for agent sandboxes")).toBeInTheDocument();
    expect(card).not.toHaveAttribute("data-settled");
  });

  it("shows a running timeline with a shimmering label while work is in progress", () => {
    view(message([{ type: "tool", id: "t", name: "web_search", status: "running", summary: "oslo" }], { status: "running" }));
    expect(screen.getByRole("button", { name: "Working" })).toBeInTheDocument();
  });

  it("shows an error with Retry, and a stopped reply", () => {
    const retry = vi.fn();
    view(message([], { status: "error", error: "Invalid API key" }), retry);
    expect(screen.getByRole("alert")).toHaveTextContent("Invalid API key");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(retry).toHaveBeenCalled();
    cleanup();
    view(message([{ type: "text", text: "partial" }], { status: "canceled" }));
    expect(screen.getByText("Stopped")).toBeInTheDocument();
  });

  it("renders a user message as plain text with attachment chips", () => {
    view(message([{ type: "attachment", attachmentId: "f", name: "notes.txt", mime: "text/plain", size: 2048 }, { type: "text", text: "**not bold**" }], { role: "user", author: "user" }));
    expect(screen.getByText("**not bold**")).toBeInTheDocument();
    expect(screen.getByText("notes.txt")).toBeInTheDocument();
    expect(screen.getByText("2 KB")).toBeInTheDocument();
  });

  describe("working line", () => {
    const working = () => document.querySelector("[data-slot=working-status]");

    it("says what a running reply is doing, from the first moment to the last", () => {
      const { unmount } = view(message([], { status: "running" }));
      expect(working()).toHaveTextContent("Thinking…");
      unmount();
      view(message([{ type: "tool", id: "t1", name: "web_search", status: "running", summary: "q" }], { status: "running" }));
      expect(working()).toHaveTextContent("Searching the web…");
    });

    it("keeps saying it is thinking between tool calls, then that it is writing", () => {
      const done = { type: "tool", id: "t1", name: "orole_get_skill", status: "done", summary: "grill-me" } as const;
      const { unmount } = view(message([done], { status: "running" }));
      expect(working()).toHaveTextContent("Thinking…");
      unmount();
      view(message([done, { type: "text", text: "Here is" }], { status: "running" }));
      expect(working()).toHaveTextContent("Writing the reply…");
    });

    it("names the bot a reply is waiting for, and a queued reply as starting", () => {
      const { unmount } = view(message([{ type: "delegation", id: "d1", name: "Coder", status: "running" }], { status: "running" }));
      expect(working()).toHaveTextContent("Waiting for Coder…");
      unmount();
      view(message([], { status: "pending" }));
      expect(working()).toHaveTextContent("Starting…");
    });

    it("adds the elapsed time once the work has gone on for a few seconds", () => {
      const { unmount } = view(message([], { status: "running", createdAt: Date.now() - 1000 }));
      expect(document.querySelector("[data-slot=working-elapsed]")).toBeNull();
      unmount();
      view(message([], { status: "running", createdAt: Date.now() - 75_000 }));
      expect(document.querySelector("[data-slot=working-elapsed]")).toHaveTextContent("1m 15s");
    });

    it("is gone once the reply has settled", () => {
      view(message([{ type: "text", text: "Done." }], { status: "complete" }));
      expect(working()).toBeNull();
    });

    it("labels skill and visual tools in plain words", () => {
      view(message([{ type: "tool", id: "t1", name: "orole_get_skill", status: "done", summary: "x" }, { type: "tool", id: "t2", name: "show_table", status: "running" }], { status: "running" }));
      expect(working()).toHaveTextContent("Preparing a visual…");
    });
  });
});

describe("report-back messages", () => {
  it("draws what another bot reported back as a collapsed card naming that bot", () => {
    const report = message([{ type: "text", text: "Grok Bot is a new app from Acme." }], { role: "user", author: "report:r" });
    render(<ChatMessageView message={report} botName="Atlas" botAvatar="" bots={[{ id: "r", name: "Researcher", avatar: "" }]} />);
    expect(screen.getByText("Researcher reported back")).toBeInTheDocument();
    expect(screen.queryByText(/Handoff by/)).toBeNull();
    expect(screen.getByText("Grok Bot is a new app from Acme.")).toBeInTheDocument();
  });
});

describe("quoted user messages", () => {
  it("draws the quoted passage as a quote above the request", () => {
    const sent = message([{ type: "text", text: "> I wrote this in a neutral register.\n\nExplain this selected text." }], { role: "user", author: "user" });
    render(<ChatMessageView message={sent} botName="Atlas" botAvatar="" />);
    const quote = document.querySelector("[data-slot=chat-quote]") as HTMLElement;
    expect(quote).toHaveTextContent("I wrote this in a neutral register.");
    expect(screen.getByText("Explain this selected text.")).toBeInTheDocument();
    expect(screen.queryByText(/^>/)).toBeNull();
  });
});

describe("documents and todo lists a reply wrote", () => {
  const doc = (title: string, content: string, version: number) => ({ type: "ui" as const, id: "write_document", name: "document", spec: JSON.stringify({ title, content, version }) });

  it("shows a document once at its latest version, opens it beside the conversation and copies its markdown", async () => {
    const writeText = vi.fn(async () => {});
    Object.assign(navigator, { clipboard: { writeText } });
    useChat.setState({ pane: null });
    view(message([doc("Plan", "# Plan\n\nfirst", 1), { type: "text", text: "Revised." }, doc("Plan", "# Plan\n\nsecond draft here", 2)]));
    const cards = document.querySelectorAll("[data-slot=document-card]");
    expect(cards).toHaveLength(1);
    expect(cards[0]).toHaveTextContent("Plan");
    expect(cards[0]).toHaveTextContent("v2 · 4 words");
    const open = screen.getByRole("button", { name: "Open" });
    expect(open).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(open);
    expect(useChat.getState().pane).toEqual({ kind: "document", messageId: "m", partIndex: 2 });
    expect(screen.getByRole("button", { name: "Open" })).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(screen.getByRole("button", { name: "Copy document" }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith("# Plan\n\nsecond draft here"));
    useChat.setState({ pane: null });
  });

  it("draws the agent's working list with its counter", () => {
    view(message([{ type: "ui", id: "update_todos", name: "todo", spec: JSON.stringify({ title: "Plan", items: [
      { id: "a", text: "Read", status: "done" }, { id: "b", text: "Fix", status: "active" }, { id: "c", text: "Skip", status: "cancelled" },
    ] }) }]));
    expect(screen.getByRole("list", { name: "Plan" })).toBeInTheDocument();
    expect(screen.getByText("1/2")).toBeInTheDocument();
  });
});
