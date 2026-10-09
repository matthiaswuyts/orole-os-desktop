import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Message } from "@/mygo";

vi.mock("@/lib/openExternal", () => ({ isWebURL: (u: string) => /^https?:/.test(u), openExternal: vi.fn() }));

import { ChatMessageView } from "./MessageBubble";

const message = (parts: Message["parts"], over: Partial<Message> = {}): Message => ({
  id: "m", botId: "a", seq: 2, role: "assistant", author: "bot:a", parts, status: "complete", createdAt: 1, ...over,
});
const view = (m: Message, onRetry?: () => void) => render(<ChatMessageView message={m} botName="Atlas" botAvatar="" onRetry={onRetry} />);
afterEach(cleanup);

describe("message parts", () => {
  it("renders text as markdown, reasoning collapsed with its duration, and tool rows with their state", () => {
    view(message([
      { type: "reasoning", text: "weighing options", durationMs: 4200 },
      { type: "tool", id: "t", name: "web_search", status: "done", durationMs: 1200, summary: "oslo flights" },
      { type: "text", text: "**Bold** answer" },
    ]));
    expect(screen.getByText("Thought for 4.2 s")).toBeInTheDocument();
    expect(screen.getByText("Searched the web")).toBeInTheDocument();
    expect(screen.getByText("1.2 s")).toBeInTheDocument();
    expect(screen.getByText("Bold").tagName).toBe("STRONG");
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

  it("shows delegation rows, memory chips and notices", () => {
    view(message([
      { type: "delegation", name: "Coder", status: "done" },
      { type: "memory", text: "likes tea" },
      { type: "notice", text: "Earlier messages were summarised." },
    ]));
    expect(screen.getByText("Asked Coder")).toBeInTheDocument();
    expect(screen.getByText("Memory updated")).toBeInTheDocument();
    expect(screen.getByRole("note")).toHaveTextContent("Earlier messages were summarised.");
  });

  it("marks a tool left running in a settled message as not finished", () => {
    view(message([{ type: "tool", id: "t", name: "web_fetch", status: "running" }]));
    expect(screen.getByText("Page read did not finish")).toBeInTheDocument();
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

  it("shows typing dots for a running reply that has produced nothing", () => {
    view(message([], { status: "running" }));
    expect(document.querySelector("[data-slot=typing-indicator]")).not.toBeNull();
  });
});
