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

function select(startId: string, endId = startId) {
  const range = document.createRange();
  range.setStart(document.getElementById(startId)!.firstChild!, 4);
  range.setEnd(document.getElementById(endId)!.firstChild!, 9);
  const sel = window.getSelection()!;
  sel.removeAllRanges();
  sel.addRange(range);
  act(() => { document.dispatchEvent(new Event("selectionchange")); });
}

beforeEach(() => {
  Range.prototype.getBoundingClientRect = () => ({ left: 100, right: 220, top: 40, bottom: 58, width: 120, height: 18, x: 100, y: 40, toJSON: () => ({}) });
  send.mockReset();
  useChatMessages.setState({ send });
});
afterEach(() => { cleanup(); window.getSelection()?.removeAllRanges(); });

describe("selection menu", () => {
  it("appears under text selected in a bot reply and sends the quoted passage with the chosen request", () => {
    mount();
    expect(screen.queryByRole("toolbar")).toBeNull();
    select("p1");
    const bar = screen.getByRole("toolbar", { name: "Ask about the selected text" });
    expect(bar.style.top).toBe("66px"); // just below the selection
    for (const name of ["Shorten", "Improve", "Explain"]) expect(screen.getByRole("button", { name })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Explain" }));
    expect(send).toHaveBeenCalledWith("a", { text: "> first\n\nExplain this selected text." });
    expect(screen.queryByRole("toolbar")).toBeNull();
    expect(window.getSelection()!.isCollapsed).toBe(true);
  });

  it("stays away from the user's own messages and from selections that cross replies", () => {
    mount();
    select("p3");
    expect(screen.queryByRole("toolbar")).toBeNull();
    select("p1", "p2");
    expect(screen.queryByRole("toolbar")).toBeNull();
  });

  it("goes away when the selection is cleared or Escape is pressed", () => {
    mount();
    select("p1");
    act(() => { window.getSelection()!.removeAllRanges(); document.dispatchEvent(new Event("selectionchange")); });
    expect(screen.queryByRole("toolbar")).toBeNull();
    select("p1");
    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByRole("toolbar")).toBeNull();
  });
});
