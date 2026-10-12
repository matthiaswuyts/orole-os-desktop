import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Session } from "@/mygo";
import type * as ActivityModule from "./activity";

const builds = vi.hoisted(() => ({ grid: 0, totals: 0 }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("./activity", async (original) => {
  const actual = await original<typeof ActivityModule>();
  return {
    ...actual,
    buildActivityGrid: (...args: Parameters<typeof actual.buildActivityGrid>) => { builds.grid++; return actual.buildActivityGrid(...args); },
    compareRange: (...args: Parameters<typeof actual.compareRange>) => { builds.totals++; return actual.compareRange(...args); },
  };
});
import { useAgents } from "@/stores/agents";
import { useDaemon } from "@/stores/daemon";
import { OverviewWidget } from "./OverviewWidget";

const started = new Date(2026, 9, 8, 12).toISOString();
const session = (n: number, patch: Partial<Session> = {}): Session => ({ id: `s${n}`, title: `Task ${n}`, status: "working", startedAt: started, updatedAt: started, activity: "", prNumber: 0, additions: 0, deletions: 0, ...patch }) as Session;

beforeEach(() => {
  builds.grid = 0;
  builds.totals = 0;
  useDaemon.setState({ link: "connected" });
  useAgents.setState({ available: true, loaded: true, offline: false, error: null, sessions: { s1: session(1), s2: session(2) } });
});
afterEach(cleanup);

describe("overview widget", () => {
  it("formats no dates while rendering or hovering, and rebuilds only when what it shows changes", () => {
    const dateCalls = vi.spyOn(Date.prototype, "toLocaleDateString");
    const stringCalls = vi.spyOn(Date.prototype, "toLocaleString");
    render(<OverviewWidget widgetType="overview" onConfig={() => {}} onRemove={() => {}} />);
    const cells = screen.getAllByRole("gridcell").filter((cell) => cell.hasAttribute("aria-label"));
    expect(cells).toHaveLength(365);
    expect(screen.getByRole("gridcell", { name: "2 tasks on October 8, 2026" })).toHaveAttribute("title", "2 tasks on October 8, 2026");
    expect(builds).toEqual({ grid: 1, totals: 1 });
    expect(dateCalls.mock.calls.length + stringCalls.mock.calls.length).toBe(0);

    fireEvent.mouseEnter(screen.getByRole("gridcell", { name: "2 tasks on October 8, 2026" }));
    expect(screen.getByText("Task 1 · Task 2")).toBeInTheDocument();
    for (let i = 0; i < 20; i++) act(() => useAgents.setState((s) => ({ sessions: { ...s.sessions, s1: session(1, { activity: `step ${i}`, updatedAt: new Date(Date.parse(started) + i * 1000).toISOString() }) } })));
    expect(builds).toEqual({ grid: 1, totals: 1 });
    expect(dateCalls.mock.calls.length + stringCalls.mock.calls.length).toBe(0);

    act(() => useAgents.setState((s) => ({ sessions: { ...s.sessions, s1: session(1, { additions: 12 }) } })));
    expect(builds).toEqual({ grid: 1, totals: 2 });

    act(() => useAgents.setState((s) => ({ sessions: { ...s.sessions, s3: session(3) } })));
    expect(builds.grid).toBe(2);
    expect(screen.getByRole("gridcell", { name: "3 tasks on October 8, 2026" })).toBeInTheDocument();
  });

  it("keeps the chosen range in the widget's config", () => {
    const onConfig = vi.fn();
    const { rerender } = render(<OverviewWidget widgetType="overview" onConfig={onConfig} onRemove={() => {}} />);
    expect(screen.getByRole("button", { name: "Range: This week" })).toBeInTheDocument();
    rerender(<OverviewWidget widgetType="overview" config={{ range: "year" }} onConfig={onConfig} onRemove={() => {}} />);
    expect(screen.getByRole("button", { name: "Range: This year" })).toBeInTheDocument();
    expect(screen.getAllByText(/vs last year$/)).toHaveLength(4);
  });
});
