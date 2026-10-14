import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it } from "vitest";
import { useChat } from "@/stores/chat";
import { CallCard } from "./CallCard";
import { callPart } from "./callFormat";

beforeEach(() => useChat.setState({ pane: null, settingsOpen: false }));
afterEach(cleanup);

it("renders a finished outbound call", () => {
  render(<CallCard part={{ type: "call", id: "c1", name: "outbound", status: "completed", title: "deploy failed", durationMs: 75_000, summary: "est. $0.15" }} />);
  expect(screen.getByText("Called you")).toBeInTheDocument();
  expect(screen.getByText("deploy failed")).toBeInTheDocument();
  expect(screen.getByText("1:15 · est. $0.15")).toBeInTheDocument();
});

it("renders a missed call", () => {
  render(<CallCard part={{ type: "call", id: "c1", name: "outbound", status: "voicemail", title: "deploy failed" }} />);
  expect(screen.getByText("Missed call")).toBeInTheDocument();
});

it("renders a call in progress", () => {
  render(<CallCard part={{ type: "call", id: "c1", name: "inbound", status: "in_progress", title: "You called" }} />);
  expect(screen.getByText("You called · on the line")).toBeInTheDocument();
});

it("shows a declined call as missed and a queued call as live", () => {
  const { rerender } = render(<CallCard part={{ type: "call", id: "c", name: "outbound", status: "rejected" }} />);
  expect(screen.getByText("Missed call")).toBeInTheDocument();
  rerender(<CallCard part={{ type: "call", id: "c", name: "outbound", status: "queued" }} />);
  expect(screen.getByText("Calling you…")).toBeInTheDocument();
});

it("is drawn as the bot's chat bubble and opens the call pane when clicked", () => {
  render(<CallCard part={{ type: "call", id: "c1", name: "outbound", status: "completed" }} />);
  const card = screen.getByRole("button", { name: /Called you/ });
  expect(card.closest("[data-slot=chat-bubble]")).not.toBeNull();
  expect(card.closest("[data-slot=chat-bubble]")!.className).not.toMatch(/\bborder\b/);
  expect(card).toHaveAttribute("aria-pressed", "false");
  fireEvent.click(card);
  expect(useChat.getState().pane).toEqual({ kind: "call", id: "c1" });
  expect(card).toHaveAttribute("aria-pressed", "true");
});

it("opens from the keyboard", () => {
  render(<CallCard part={{ type: "call", id: "c2", name: "inbound", status: "completed" }} />);
  const card = screen.getByRole("button", { name: /You called/ });
  card.focus();
  expect(card).toHaveFocus();
  // A native button activates on Enter/Space; the click is what the browser dispatches.
  fireEvent.click(card);
  expect(useChat.getState().pane).toEqual({ kind: "call", id: "c2" });
});

it("turns a stored call into a card part like the chat does", () => {
  expect(callPart({ id: "c1", botId: "b", direction: "outbound", reason: "why", status: "completed", answeredAt: 1000, endedAt: 61_000, estCostMicros: 150_000, createdAt: 1 }))
    .toEqual({ type: "call", id: "c1", name: "outbound", status: "completed", title: "why", durationMs: 60_000, summary: "est. $0.15" });
});
