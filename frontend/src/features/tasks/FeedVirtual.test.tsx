import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Event as AgentEvent, Session } from "@/mygo";

vi.mock("@/mygo", () => ({ Agents: { approve: vi.fn(), get: vi.fn(), artifact: vi.fn() }, Shell: { openExternal: vi.fn() }, events: { agentsEvent: { on: () => () => {} } } }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/stores/agents", () => ({ useAgents: { getState: () => ({ upsert: vi.fn() }) } }));
vi.mock("@/stores/boxes", () => ({ useBoxes: vi.fn() }));
import { Feed, VIRTUALIZE_FROM } from "./Feed";

let seq = 0;
const at = "2026-10-08T12:00:00Z";
const message = (text: string): AgentEvent => ({ id: ++seq, sessionId: "s1", at, kind: "message", text, data: null });
const session = { id: "s1", live: true, status: "idle", activity: "", pending: [] } as unknown as Session;
const conversation = (count: number, special?: { index: number; text: string }) => Array.from({ length: count }, (_, i) => message(special?.index === i ? special.text : `message ${i}`));
const props = { session, loading: false, loadingMore: false, error: null, hasMore: false, onLoadMore: () => Promise.resolve(true) };
const observers = new Set<(entries: unknown[]) => void>();
/** What the browser does after layout: reports size changes to every ResizeObserver, then the scroll position the pin produced. */
const layout = () => act(() => { observers.forEach((callback) => callback([])); fireEvent.scroll(screen.getByRole("log")); });
/** A feed opens empty and receives its history afterwards, as TaskView does. */
function openWith(events: AgentEvent[]) {
  const view = render(<Feed {...props} events={[]} />);
  view.rerender(<Feed {...props} events={events} />);
  return view;
}
const rows = () => screen.getAllByRole("log")[0]!.querySelectorAll("[data-scroll-anchor]");
const log = () => screen.getByRole("log");

beforeEach(() => {
  seq = 0;
  observers.clear();
  vi.stubGlobal("ResizeObserver", class { callback: (entries: unknown[]) => void; constructor(callback: (entries: unknown[]) => void) { this.callback = callback; observers.add(callback); } observe() {} unobserve() {} disconnect() { observers.delete(this.callback); } });
  vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockImplementation(function (this: HTMLElement) { return this.dataset.index === undefined ? 600 : 100; });
  vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockReturnValue(800);
  // The scroll height of the log is the virtual list's, and content moves up as the log scrolls, like a real layout.
  vi.spyOn(HTMLElement.prototype, "scrollHeight", "get").mockImplementation(function (this: HTMLElement) {
    const list = this.getAttribute("role") === "log" ? (this.querySelector("[data-index]")?.parentElement as HTMLElement | null) : null;
    return list ? parseFloat(list.style.height) + 100 : 0;
  });
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
    const scrolled = this.closest<HTMLElement>("[role=log]")?.scrollTop ?? 0;
    const row = this.closest<HTMLElement>("[data-index]");
    const top = this.getAttribute("role") === "log" ? 0 : (row ? parseFloat(/translateY\(([-\d.]+)px/.exec(row.style.transform)?.[1] ?? "0") : 0) - scrolled;
    return { top, bottom: top + 100, height: 100, left: 0, right: 800, width: 800, x: 0, y: top, toJSON() {} };
  });
  vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(600);
  Object.defineProperty(HTMLElement.prototype, "scrollTo", { configurable: true, value(this: HTMLElement, options: ScrollToOptions) { this.scrollTop = options.top ?? 0; this.dispatchEvent(new Event("scroll")); } });
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); Reflect.deleteProperty(HTMLElement.prototype, "scrollTo"); });

describe("virtualized feed", () => {
  it("renders every row of a short conversation", () => {
    openWith(conversation(VIRTUALIZE_FROM));
    expect(rows()).toHaveLength(VIRTUALIZE_FROM);
  });

  it("renders only the rows near the viewport of a long conversation, pinned to the newest", () => {
    openWith(conversation(400));
    layout();
    layout();
    expect(rows().length).toBeGreaterThan(0);
    expect(rows().length).toBeLessThan(40);
    expect(screen.getByText("message 399")).toBeInTheDocument();
    expect(screen.queryByText("message 0")).toBeNull();
    // The list keeps the height of all 400 rows, so the scrollbar still spans the whole conversation.
    expect(parseFloat((log().querySelector("[data-index]")?.parentElement as HTMLElement).style.height)).toBeGreaterThan(400 * 100);
  });

  it("scrolls a search match outside the rendered window into view and highlights it", () => {
    openWith(conversation(400, { index: 3, text: "the needle" }));
    layout();
    layout();
    expect(screen.queryByText("the needle")).toBeNull();
    fireEvent.keyDown(log(), { key: "f", ctrlKey: true }); // the search opens on Ctrl+F
    fireEvent.change(screen.getByRole("searchbox", { name: "Find in conversation" }), { target: { value: "needle" } });
    layout();
    expect(screen.getByText("the needle")).toBeInTheDocument();
    expect(log().querySelector("[data-search-active]")).toHaveTextContent("the needle");
    expect(screen.queryByText("message 399")).toBeNull();
  });

  it("opens a collapsed run far outside the window to show the search match inside it", () => {
    // 100 turns: a prompt, interim commentary, a tool call and its result, then the closing reply, folded into one run each.
    const turns = Array.from({ length: 100 }, (_, i) => [
      { id: ++seq, sessionId: "s1", at, kind: "prompt", text: `prompt ${i}`, data: null },
      message(`interim note ${i}`),
      { id: ++seq, sessionId: "s1", at, kind: "tool", text: "", data: { id: `t${i}`, name: "Read", status: "running", input: { path: `f${i}.ts` } } },
      { id: ++seq, sessionId: "s1", at, kind: "tool_result", text: "", data: { id: `t${i}`, name: "Read", status: "ok", output: "ok" } },
      message(`closing ${i}`),
    ] as AgentEvent[]).flat();
    openWith(turns);
    layout();
    layout();
    expect(screen.queryByText("interim note 3")).toBeNull();
    fireEvent.keyDown(log(), { key: "f", ctrlKey: true });
    fireEvent.change(screen.getByRole("searchbox", { name: "Find in conversation" }), { target: { value: "interim note 3" } });
    layout();
    layout();
    const match = log().querySelector("[data-search-active]");
    expect(match).toHaveTextContent("interim note 3");
    expect(match?.closest('[data-slot="agent-run-tree"]')?.querySelector('[data-slot="collapsible-trigger"]')).toHaveAttribute("aria-expanded", "true");
  });

  it("keeps the reader's place when older history is prepended and the row they read is no longer in the rendered window", () => {
    const all = conversation(400);
    const view = openWith(all.slice(100));
    layout();
    layout();
    log().scrollTop = 0; // the reader scrolls to the top of the loaded history
    act(() => { fireEvent.scroll(log()); });
    fireEvent.keyDown(log(), { key: "PageUp" }); // the reader is now away from the newest message
    const reading = log().querySelector<HTMLElement>("[data-scroll-anchor]")!.dataset.scrollAnchor;
    expect(reading).toBe(String(all[100]!.id));
    view.rerender(<Feed {...props} events={all} />);
    // The row now sits below 100 new rows (96px estimate + 16px gap each) that are not rendered; the viewport follows it there.
    expect(log().scrollTop).toBe(100 * (96 + 16));
    layout();
    expect(log().querySelector(`[data-scroll-anchor="${reading}"]`)).not.toBeNull();
  });
});
