import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AutomationSignal } from "@/mygo";

// vi.mock is hoisted above static imports, so the shared bridge mock must be loaded dynamically (see builder/testing.ts).
vi.mock("@/mygo", async () => (await import("../builder/testing")).mygoMock);

import { bridge, resetBridge } from "../builder/testing";
import { AutomationIdeas } from "./AutomationIdeas";

const signal = (key: string, over: Partial<AutomationSignal> = {}): AutomationSignal => ({
  botId: "b1", key, label: `mcp__github__workflow_runs {"repo":"acme/${key}"}`, count: 4, days: 2, ready: true, answered: false, firstAt: 1, lastAt: 2, ...over,
});

beforeEach(() => {
  resetBridge();
  bridge.chat.automationSignals.mockResolvedValue([signal("app"), signal("web", { ready: false, answered: true, count: 1, days: 1 })]);
});
afterEach(cleanup);

describe("automation ideas", () => {
  it("lists what the bot noticed, how often, and whether it may still suggest it", async () => {
    render(<AutomationIdeas botId="b1" />);
    expect(await screen.findByText(/acme\/app/)).toBeInTheDocument();
    expect(bridge.chat.automationSignals).toHaveBeenCalledWith("b1");
    expect(screen.getByText(/4 times on 2 days/)).toBeInTheDocument();
    expect(screen.getByText("May suggest")).toBeInTheDocument();
    expect(screen.getByText(/Once on 1 day/)).toBeInTheDocument();
    expect(screen.getByText("Answered")).toBeInTheDocument();
  });

  it("forgets an idea in the daemon and drops it from the list", async () => {
    bridge.chat.deleteAutomationSignal.mockResolvedValue(undefined);
    render(<AutomationIdeas botId="b1" />);
    fireEvent.click(await screen.findByRole("button", { name: /Forget idea: .*acme\/app/ }));
    expect(bridge.chat.deleteAutomationSignal).toHaveBeenCalledWith({ botId: "b1", key: "app" });
    await waitFor(() => expect(screen.queryByText(/acme\/app/)).toBeNull());
    expect(screen.getByText(/acme\/web/)).toBeInTheDocument();
  });
});
