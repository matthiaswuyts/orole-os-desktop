import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Event as AgentEvent, Session } from "@/mygo";
import type * as TaskEventsModule from "@/lib/taskEvents";
import type * as FeedItemsModule from "./FeedItems";
import type * as RunTreeModule from "@/components/assistant-ui/elements/agent-run-tree";

const api = vi.hoisted(() => ({
  transcript: vi.fn(),
  listeners: new Set<(e: AgentEvent) => void>(),
  renders: { assistant: 0, run: 0 },
  groups: 0,
}));
vi.mock("@/mygo", () => ({
  Agents: { approve: vi.fn(), get: vi.fn(), artifact: vi.fn(), diff: vi.fn(), transcript: api.transcript },
  Shell: { openExternal: vi.fn() },
  events: { agentsEvent: { on: (f: (e: AgentEvent) => void) => { api.listeners.add(f); return () => api.listeners.delete(f); } } },
}));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/stores/agents", () => ({ useAgents: { getState: () => ({ upsert: vi.fn() }) } }));
vi.mock("@/stores/boxes", () => ({ useBoxes: vi.fn() }));
vi.mock("./Composer", () => ({ Composer: () => null }));
vi.mock("@/lib/taskEvents", async (original) => {
  const actual = await original<typeof TaskEventsModule>();
  return { ...actual, groupEvents: (...args: Parameters<typeof actual.groupEvents>) => { api.groups++; return actual.groupEvents(...args); } };
});
vi.mock("./FeedItems", async (original) => {
  const actual = await original<typeof FeedItemsModule>();
  return {
    ...actual,
    AssistantMessage: (props: Parameters<typeof actual.AssistantMessage>[0]) => { api.renders.assistant++; return actual.AssistantMessage(props); },
  };
});
vi.mock("@/components/assistant-ui/elements/agent-run-tree", async (original) => {
  const actual = await original<typeof RunTreeModule>();
  return { ...actual, AgentRunTree: (props: Parameters<typeof actual.AgentRunTree>[0]) => { api.renders.run++; return actual.AgentRunTree(props); } };
});
import { TaskView } from "./TaskView";

let seq = 0;
const at = "2026-10-08T12:00:00Z";
const ev = (kind: string, text = "", data: unknown = null): AgentEvent => ({ id: ++seq, sessionId: "s1", at, kind, text, data });
const session = (patch: Partial<Session> = {}): Session => ({ id: "s1", live: true, status: "working", activity: "", pending: [], mode: "structured", machine: "local", updatedAt: at, error: "", ...patch }) as Session;

/** A 500-event history: five events per step (prompt, reply, a tool call and its result, a closing reply). */
function history(): AgentEvent[] {
  const out: AgentEvent[] = [];
  for (let i = 0; i < 100; i++) {
    out.push(ev("prompt", `prompt ${i}`), ev("message", `reply ${i} with some **markdown**`));
    out.push(ev("tool", "", { id: `t${i}`, name: "Read", status: "running", input: { path: `f${i}.ts` } }), ev("tool_result", "", { id: `t${i}`, name: "Read", status: "ok", output: "ok" }));
    out.push(ev("message", `done ${i}`));
  }
  return out;
}

beforeEach(() => {
  seq = 0;
  api.listeners.clear();
  api.renders.assistant = api.renders.run = api.groups = 0;
  // jsdom has no layout: an 800px viewport and 120px rows give the virtualizer a window to compute.
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockImplementation(function (this: HTMLElement) { return this.dataset.index === undefined ? 800 : 120; });
  vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockReturnValue(900);
  // The scroll height of the log is the virtual list height, so a pinned reader follows the newest row.
  vi.spyOn(HTMLElement.prototype, "scrollHeight", "get").mockImplementation(function (this: HTMLElement) {
    const list = this.getAttribute("role") === "log" ? (this.querySelector("[data-index]")?.parentElement as HTMLElement | null) : null;
    return list ? parseFloat(list.style.height) + 100 : 0;
  });
  // Content moves up as the log scrolls, like a real layout.
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
    const top = this.getAttribute("role") === "log" ? 0 : -(this.closest<HTMLElement>("[role=log]")?.scrollTop ?? 0);
    return { top, bottom: top + 120, height: 120, left: 0, right: 900, width: 900, x: 0, y: top, toJSON() {} };
  });
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

describe("task view under an event flood", () => {
  it("groups each event once and re-renders only the rows near the viewport while 100 live events stream into a 500-event history", async () => {
    api.transcript.mockResolvedValue(history());
    let s = session();
    const view = render(<TaskView session={s} />);
    await act(async () => { await Promise.resolve(); });
    api.groups = api.renders.assistant = api.renders.run = 0;

    const started = performance.now();
    for (let i = 0; i < 100; i++) {
      const next = i % 3 === 0 ? ev("usage", "", { tokens: i }) : i % 3 === 1 ? ev("message", `streamed ${i}`) : ev("tool", "", { id: `live${i}`, name: "Bash", status: "running", input: { command: "ls" } });
      s = session({ updatedAt: new Date(Date.parse(at) + i * 1000).toISOString() });
      act(() => { for (const f of api.listeners) f(next); view.rerender(<TaskView session={s} />); });
      act(() => { fireEvent.scroll(screen.getByRole("log")); }); // the browser reports the pinned reader's new scroll position
    }
    const elapsed = Math.round(performance.now() - started);
    console.log("FLOOD", JSON.stringify({ groups: api.groups, ...api.renders, ms: elapsed }));
    // origin/main measured on this scenario: 200 groupings (feed and canvas each), 21683 assistant and 11650 tool renders (before assistant turns folded into runs).
    expect(api.groups).toBe(100);
    expect(api.renders.assistant).toBeLessThan(100);
    expect(api.renders.run).toBeLessThan(100);
  }, 30_000); // counts renders, not time: a 600-event history takes ~6 s on a CI runner
});
