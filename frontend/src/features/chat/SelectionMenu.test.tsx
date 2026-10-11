import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { createRef } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", async () => (await import("./builder/testing")).mygoMock);

import { useChatMessages } from "@/stores/chatMessages";
import { SelectionMenu } from "./SelectionMenu";

const send = vi.fn();

function mount() {
  const root = createRef<HTMLDivElement>();
  render(<div ref={root}>
    <div id="bot1" data-slot="chat-bubble"><p id="p1">The first reply paragraph.</p></div>
    <div id="bot2" data-slot="chat-bubble"><p id="p2">The second reply paragraph.</p></div>
    <div id="mine" data-slot="chat-user"><p id="p3">My own message.</p></div>
    <SelectionMenu botId="a" root={root} />
  </div>);
}

/** Selects with the mouse: press, drag (the selection changes), release. */
function select(startId: string, endId = startId) {
  const el = document.getElementById(startId)!;
  fireEvent.mouseDown(el);
  setSelection(startId, endId);
  fireEvent.mouseUp(el);
  act(() => { vi.runOnlyPendingTimers(); });
}

function setSelection(startId: string, endId = startId) {
  const range = document.createRange();
  range.setStart(document.getElementById(startId)!.firstChild!, 4);
  range.setEnd(document.getElementById(endId)!.firstChild!, 9);
  const sel = window.getSelection()!;
  sel.removeAllRanges();
  sel.addRange(range);
  act(() => { document.dispatchEvent(new Event("selectionchange")); });
}

beforeEach(() => {
  vi.useFakeTimers();
  Range.prototype.getBoundingClientRect = () => ({ left: 100, right: 220, top: 40, bottom: 58, width: 120, height: 18, x: 100, y: 40, toJSON: () => ({}) });
  send.mockReset();
  useChatMessages.setState({ send });
});
afterEach(() => { vi.useRealTimers(); cleanup(); window.getSelection()?.removeAllRanges(); });

describe("selection menu", () => {
  it("appears under text selected in a bot reply and sends the quoted passage with the chosen request", () => {
    mount();
    expect(screen.queryByRole("group")).toBeNull();
    select("p1");
    const bar = screen.getByRole("group", { name: "Ask about the selected text" });
    expect((bar.parentElement as HTMLElement).style.top).toBe("66px"); // just below the selection
    for (const name of ["Add to chat", "Shorten", "Improve", "Explain"]) expect(screen.getByRole("button", { name })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Explain" }));
    expect(send).toHaveBeenCalledWith("a", { text: "> first\n\nExplain this selected text." });
    expect(screen.queryByRole("group")).toBeNull();
    expect(window.getSelection()!.isCollapsed).toBe(true);
  });

  it("adds the quoted passage to the message box without sending, keeping what is already typed", () => {
    mount();
    useChatMessages.setState({ drafts: { a: { text: "Regarding this:", research: false } } });
    select("p1");
    fireEvent.click(screen.getByRole("button", { name: "Add to chat" }));
    expect(send).not.toHaveBeenCalled();
    expect(useChatMessages.getState().drafts.a?.text).toBe("Regarding this:\n\n> first\n\n");
    expect(screen.queryByRole("group")).toBeNull();
  });

  it("stays away from the user's own messages and from selections that cross replies", () => {
    mount();
    select("p3");
    expect(screen.queryByRole("group")).toBeNull();
    select("p1", "p2");
    expect(screen.queryByRole("group")).toBeNull();
  });

  it("waits until the mouse is released: nothing shows while the selection is still being dragged", () => {
    mount();
    fireEvent.mouseDown(document.getElementById("p1")!);
    setSelection("p1");
    act(() => { vi.advanceTimersByTime(2000); });
    expect(screen.queryByRole("group")).toBeNull();
    fireEvent.mouseUp(document.getElementById("p1")!);
    act(() => { vi.runOnlyPendingTimers(); });
    expect(screen.getByRole("group", { name: "Ask about the selected text" })).toBeInTheDocument();
  });

  it("appears after a keyboard selection has been still for a moment, not while it grows", () => {
    mount();
    setSelection("p1");
    expect(screen.queryByRole("group")).toBeNull();
    act(() => { vi.advanceTimersByTime(200); });
    setSelection("p1"); // still changing: the wait starts over
    act(() => { vi.advanceTimersByTime(200); });
    expect(screen.queryByRole("group")).toBeNull();
    act(() => { vi.advanceTimersByTime(200); });
    expect(screen.getByRole("group", { name: "Ask about the selected text" })).toBeInTheDocument();
  });

  it("goes away when the selection is cleared or Escape is pressed", () => {
    mount();
    select("p1");
    act(() => { window.getSelection()!.removeAllRanges(); document.dispatchEvent(new Event("selectionchange")); vi.runOnlyPendingTimers(); });
    expect(screen.queryByRole("group")).toBeNull();
    select("p1");
    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByRole("group")).toBeNull();
  });
});
