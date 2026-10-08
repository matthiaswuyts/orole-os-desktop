import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useCallback } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FeedItem } from "@/lib/taskEvents";
import { conversationHits, stepSearchHit } from "./ConversationSearch";
import { conversationDay, conversationTimeline } from "./ConversationTimeline";
import { ScrollAnchor, useScrollAnchor } from "./ScrollAnchor";

const message = (key: string, text: string, at = "2026-10-08T12:00:00Z"): FeedItem => ({ key, at, type: "assistant", text });

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

describe("conversation navigation data", () => {
  it("matches literal punctuation case-insensitively, with unique occurrences and finite single-row positions", () => {
    const hits = conversationHits([message("m", "A [x] and [X].")], "[x]");
    expect(hits.map((hit) => [hit.id, hit.match, hit.position])).toEqual([["m:2", "[x]", 0], ["m:10", "[X]", 0]]);
    expect(conversationHits([message("m", "anything")], "   ")).toEqual([]);
    expect(conversationHits([{ key: "t", at: "", type: "reasoning", text: "private", reasoningId: "r" }], "private")).toEqual([]);
  });

  it("keeps original offsets after Unicode case folding and wraps both directions", () => {
    expect(conversationHits([message("m", "İ find")], "find")[0]).toMatchObject({ id: "m:2", match: "find", before: "İ " });
    expect(stepSearchHit(0, -1, 3)).toBe(2);
    expect(stepSearchHit(2, 1, 3)).toBe(0);
    expect(stepSearchHit(0, 1, 0)).toBe(-1);
  });

  it("uses local calendar days including year, rejects invalid dates, and preserves tool/message order", () => {
    const local = new Date(2026, 9, 8, 23, 59);
    const next = new Date(2026, 9, 9, 0, 1);
    expect(conversationDay(local.toISOString())).toBe("2026-10-8");
    expect(conversationDay(next.toISOString())).toBe("2026-10-9");
    expect(conversationDay("invalid")).toBeNull();
    expect(conversationDay(new Date(2027, 9, 8).toISOString())).not.toBe(conversationDay(local.toISOString()));
    const tool = (key: string, at: string): FeedItem => ({ key, at, type: "tool", call: { id: key, linked: true, name: "Read", server: "", status: "unknown", input: null, output: "", summary: "" } });
    const entries = conversationTimeline([tool("1", local.toISOString()), tool("2", local.toISOString()), message("3", "Interleaved", local.toISOString()), tool("4", local.toISOString()), tool("5", next.toISOString())]);
    expect(entries.map((entry) => entry.type === "tools" ? entry.items.map((item) => item.key) : entry.key)).toEqual([["1", "2"], "3", ["4"], ["5"]]);
  });
});

interface Row { id: number; height: number }
let resized: () => void;
function AnchoredContent({ rows }: { rows: Row[] }) {
  const scroll = useScrollAnchor(rows, rows.map((row) => row.id));
  const ref = useCallback((el: HTMLDivElement | null) => {
    scroll.viewport.current = el;
    if (!el) return;
    let top = 0;
    Object.defineProperties(el, {
      clientHeight: { configurable: true, get: () => 200 },
      scrollHeight: { configurable: true, get: () => Array.from(el.querySelectorAll<HTMLElement>("[data-height]")).reduce((sum, row) => sum + Number(row.dataset.height), 0) },
      scrollTop: { configurable: true, get: () => top, set: (value: number) => { top = Math.max(0, Math.min(value, el.scrollHeight - 200)); } },
    });
  }, [scroll.viewport]);
  return <><div ref={ref} data-testid="viewport" tabIndex={0} onScroll={scroll.onScroll} onKeyDown={scroll.onKeyDown}>
    <div ref={scroll.content}>{rows.map((row) => <div key={row.id} data-scroll-anchor={row.id} data-height={row.height}>{row.id}</div>)}</div>
  </div>{scroll.away && <ScrollAnchor unseen={scroll.unseen} onJump={scroll.jump} />}</>;
}

describe("real-content scroll anchor", () => {
  beforeEach(() => {
    vi.stubGlobal("ResizeObserver", class {
      constructor(callback: () => void) { resized = callback; }
      observe() {}
      disconnect() {}
    });
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
      let top = 0;
      const height = Number(this.dataset.height ?? 200);
      if (this.dataset.scrollAnchor) {
        for (let row = this.previousElementSibling as HTMLElement | null; row; row = row.previousElementSibling as HTMLElement | null) top += Number(row.dataset.height);
        top -= this.parentElement!.parentElement!.scrollTop;
      }
      return { top, bottom: top + height, height, left: 0, right: 100, width: 100, x: 0, y: top, toJSON() {} } as DOMRect;
    });
  });

  it("follows real row resizes while pinned, not only changes in event count", () => {
    render(<AnchoredContent rows={[{ id: 1, height: 300 }, { id: 2, height: 300 }]} />);
    const viewport = screen.getByTestId("viewport");
    expect(viewport.scrollTop).toBe(400);
    viewport.querySelector<HTMLElement>('[data-scroll-anchor="2"]')!.dataset.height = "450";
    act(() => resized());
    expect(viewport.scrollTop).toBe(550);
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("preserves an upward reader through streaming growth, prepends and asynchronous image resizing", () => {
    const rows = [{ id: 2, height: 300 }, { id: 3, height: 300 }];
    const view = render(<AnchoredContent rows={rows} />);
    const viewport = screen.getByTestId("viewport");
    viewport.scrollTop = 350;
    fireEvent.scroll(viewport);
    expect(screen.getByRole("button", { name: "Jump to latest" })).toBeInTheDocument();
    view.rerender(<AnchoredContent rows={[...rows, { id: 4, height: 100 }]} />);
    expect(viewport.scrollTop).toBe(350);
    expect(screen.getByRole("button", { name: /1 new update/ })).toBeInTheDocument();
    view.rerender(<AnchoredContent rows={[{ id: 1, height: 150 }, ...rows, { id: 4, height: 100 }]} />);
    expect(viewport.scrollTop).toBe(500);
    expect(screen.getByRole("button", { name: /1 new update/ })).toBeInTheDocument();
    viewport.querySelector<HTMLElement>('[data-scroll-anchor="2"]')!.dataset.height = "380";
    act(() => resized());
    expect(viewport.scrollTop).toBe(580);
    fireEvent.click(screen.getByRole("button", { name: /1 new update/ }));
    expect(viewport.scrollTop).toBe(730);
    expect(viewport).toHaveFocus();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("lets keyboard readers stop following and explicitly resume without animation", () => {
    const rows = [{ id: 1, height: 300 }, { id: 2, height: 300 }];
    const view = render(<AnchoredContent rows={rows} />);
    const viewport = screen.getByTestId("viewport");
    fireEvent.keyDown(viewport, { key: "PageUp" });
    view.rerender(<AnchoredContent rows={[...rows, { id: 3, height: 100 }]} />);
    expect(viewport.scrollTop).toBe(400);
    fireEvent.keyDown(viewport, { key: "End", ctrlKey: true });
    expect(viewport.scrollTop).toBe(500);
    expect(screen.queryByRole("button")).toBeNull();
  });
});
