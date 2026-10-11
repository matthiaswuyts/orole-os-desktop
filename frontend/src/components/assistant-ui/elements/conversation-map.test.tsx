import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ConversationMap } from "./conversation-map";

afterEach(cleanup);
const entries = [{ id: "a", title: "First turn", preview: "First reply", length: 2 }, { id: "b", title: "Second turn", length: 8 }, { id: "c", title: "Third turn" }];

it("marks the turn being read, jumps on click, and makes only that tick a tab stop", () => {
  const onSelect = vi.fn();
  render(<ConversationMap entries={entries} activeId="b" visibleIds={["a", "b"]} onSelect={onSelect} />);
  const ticks = screen.getAllByRole("button");
  expect(ticks.map((tick) => tick.getAttribute("aria-label"))).toEqual(["Turn 1: First turn", "Turn 2: Second turn", "Turn 3: Third turn"]);
  expect(ticks[1]).toHaveAttribute("aria-current", "location");
  expect(ticks.map((tick) => tick.tabIndex)).toEqual([-1, 0, -1]);
  fireEvent.click(ticks[2]!);
  expect(onSelect).toHaveBeenCalledWith("c");
});

it("moves between ticks with the arrow keys and falls back to the first tick when the active turn is unknown", () => {
  render(<ConversationMap entries={entries} activeId="gone" />);
  const ticks = screen.getAllByRole("button");
  expect(ticks[0]!.tabIndex).toBe(0);
  ticks[0]!.focus();
  fireEvent.keyDown(ticks[0]!, { key: "ArrowDown" });
  expect(ticks[1]).toHaveFocus();
  fireEvent.keyDown(ticks[1]!, { key: "End" });
  expect(ticks[2]).toHaveFocus();
  fireEvent.keyDown(ticks[2]!, { key: "ArrowDown" });
  expect(ticks[2]).toHaveFocus();
});

it("shows the preview card only for the tick being pointed at, and keeps it out of the accessibility tree", () => {
  render(<ConversationMap entries={entries} side="left" />);
  expect(screen.queryByText("First reply")).toBeNull();
  const first = screen.getAllByRole("listitem")[0]!;
  fireEvent.mouseEnter(first);
  expect(screen.getByText("First reply").closest("[aria-hidden]")).not.toBeNull();
  fireEvent.mouseLeave(first);
  expect(screen.queryByText("First reply")).toBeNull();
  expect(screen.getByRole("navigation", { name: "Conversation map" })).toBeInTheDocument();
});
