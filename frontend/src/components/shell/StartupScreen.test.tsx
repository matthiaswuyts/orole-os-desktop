import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useSettings } from "@/stores/settings";
import { useWorkspaces } from "@/stores/workspaces";
import { useAgents } from "@/stores/agents";
import { useDaemon } from "@/stores/daemon";
import { StartupScreen } from "./StartupScreen";

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => setTimeout(() => callback(performance.now()), 16));
  vi.stubGlobal("cancelAnimationFrame", clearTimeout);
  useSettings.setState({ loaded: false, error: null });
  useWorkspaces.setState({ loaded: false, error: null });
  useAgents.setState({ loaded: false, error: null });
  useDaemon.setState({ link: "unknown", error: null });
});
afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); });

it("waits for every startup check and never advances just because time passes", () => {
  render(<StartupScreen><button>App action</button></StartupScreen>);
  expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "0");
  act(() => { vi.advanceTimersByTime(5000); });
  expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "0");
  act(() => { useWorkspaces.setState({ loaded: true }); useDaemon.setState({ link: "connected" }); });
  expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "67");
  expect(screen.queryByRole("button", { name: "App action" })).toBeNull();
  act(() => { useSettings.setState({ loaded: true }); });
  expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "100");
  act(() => { vi.advanceTimersByTime(40); });
  expect(screen.getByRole("button", { name: "App action" })).toBeVisible();
  // A refresh or reconnect must not put an already-open app behind the startup screen.
  act(() => { useSettings.setState({ loaded: false }); useDaemon.setState({ link: "offline" }); });
  expect(screen.queryByRole("progressbar")).toBeNull();
});

it("does not wait for the task list, which can be large and has its own loading states", () => {
  render(<StartupScreen><p>App ready</p></StartupScreen>);
  act(() => {
    useSettings.setState({ loaded: true });
    useWorkspaces.setState({ loaded: true });
    useDaemon.setState({ link: "connected" });
  });
  expect(useAgents.getState().loaded).toBe(false);
  expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "100");
  act(() => { vi.advanceTimersByTime(40); });
  expect(screen.getByText("App ready")).toBeVisible();
});

it("hands off after failed initial requests instead of trapping the user at startup", () => {
  render(<StartupScreen><p>App error handling</p></StartupScreen>);
  act(() => {
    useSettings.setState({ loaded: true, error: "Settings unavailable" });
    useWorkspaces.setState({ loaded: true, error: "Workspaces unavailable" });
    useAgents.setState({ loaded: true, error: "Tasks unavailable" });
    useDaemon.setState({ link: "offline", error: "Daemon unavailable" });
  });
  expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "100");
  act(() => { vi.advanceTimersByTime(40); });
  expect(screen.getByText("App error handling")).toBeVisible();
  expect(useSettings.getState().error).toBe("Settings unavailable");
  expect(useWorkspaces.getState().error).toBe("Workspaces unavailable");
  expect(useAgents.getState().error).toBe("Tasks unavailable");
  expect(useDaemon.getState().link).toBe("offline");
});
