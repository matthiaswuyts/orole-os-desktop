import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Session } from "@/mygo";
import type * as ActivityModule from "./activity";

const builds = vi.hoisted(() => ({ count: 0 }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("./activity", async (original) => {
  const actual = await original<typeof ActivityModule>();
  return { ...actual, buildActivityGrid: (...args: Parameters<typeof actual.buildActivityGrid>) => { builds.count++; return actual.buildActivityGrid(...args); } };
});
import { useAgents } from "@/stores/agents";
import { useDaemon } from "@/stores/daemon";
import { ActivityHeatmapWidget } from "./ActivityHeatmapWidget";

const started = new Date(2026, 9, 8, 12).toISOString();
const session = (n: number, patch: Partial<Session> = {}): Session => ({ id: `s${n}`, title: `Task ${n}`, status: "working", startedAt: started, updatedAt: started, activity: "", ...patch }) as Session;

beforeEach(() => {
  builds.count = 0;
  useDaemon.setState({ link: "connected" });
  useAgents.setState({ available: true, loaded: true, offline: false, error: null, sessions: { s1: session(1), s2: session(2) } });
});
afterEach(cleanup);

describe("activity heatmap", () => {
  it("formats no dates while rendering or hovering, and rebuilds only when a session start or title changes", () => {
    const dateCalls = vi.spyOn(Date.prototype, "toLocaleDateString");
    const stringCalls = vi.spyOn(Date.prototype, "toLocaleString");
    render(<ActivityHeatmapWidget widgetType="activity-heatmap" onConfig={() => {}} onRemove={() => {}} />);
    const cells = screen.getAllByRole("gridcell").filter((cell) => cell.hasAttribute("aria-label"));
    expect(cells).toHaveLength(365);
    expect(screen.getByRole("gridcell", { name: "2 tasks on October 8, 2026" })).toHaveAttribute("title", "2 tasks on October 8, 2026");
    expect(builds.count).toBe(1);
    expect(dateCalls.mock.calls.length + stringCalls.mock.calls.length).toBe(0);

    fireEvent.mouseEnter(screen.getByRole("gridcell", { name: "2 tasks on October 8, 2026" }));
    expect(screen.getByText("Task 1 · Task 2")).toBeInTheDocument();
    for (let i = 0; i < 20; i++) act(() => useAgents.setState((s) => ({ sessions: { ...s.sessions, s1: session(1, { activity: `step ${i}`, updatedAt: new Date(Date.parse(started) + i * 1000).toISOString() }) } })));
    expect(builds.count).toBe(1);
    expect(dateCalls.mock.calls.length + stringCalls.mock.calls.length).toBe(0);

    act(() => useAgents.setState((s) => ({ sessions: { ...s.sessions, s3: session(3) } })));
    expect(builds.count).toBe(2);
    expect(screen.getByRole("gridcell", { name: "3 tasks on October 8, 2026" })).toBeInTheDocument();
  });
});
