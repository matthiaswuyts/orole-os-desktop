import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Definition, StepType } from "./api";

const mocks = vi.hoisted(() => ({ save: vi.fn(), run: vi.fn(), preview: vi.fn(), runs: vi.fn(), detail: vi.fn(), cancel: vi.fn() }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("./api", () => ({ api: mocks, errorText: (error: unknown) => error instanceof Error ? error.message : String(error) }));
vi.mock("@/stores/chat", () => {
  const state = { bots: [{ id: "b1", name: "Reviewer" }], available: false, ready: true, loading: false, load: vi.fn(), open: vi.fn() };
  return { useChat: Object.assign((select: (s: typeof state) => unknown) => select(state), { getState: () => state }) };
});
vi.mock("@/stores/projects", () => {
  const state = { projects: [], loaded: true, loading: false, load: vi.fn() };
  return { useProjects: Object.assign((select: (s: typeof state) => unknown) => select(state), { getState: () => state }) };
});
vi.mock("@/features/agents/useHarnessOptions", () => ({ useHarnessOptions: () => ({ options: { harnesses: [] }, error: undefined, loading: false, offline: false }) }));
vi.mock("@/features/projects/tasks", () => ({ openTask: vi.fn() }));
import { FlowBuilder } from "./FlowBuilder";

const field = (key: string, label: string, kind: string, required = true) => ({ key, label, kind, required, options: [], hint: "" });
const types: StepType[] = [
  { type: "bot.tell", label: "Ask a bot", group: "Bots", description: "", available: true, reason: "", sideEffect: true, keySupported: false, fields: [field("botId", "Bot", "bot"), field("message", "Message", "text")] },
  { type: "condition", label: "Condition", group: "Logic", description: "", available: true, reason: "", sideEffect: false, keySupported: false, fields: [field("condition", "Condition", "expr")] },
];
const step = { name: "", when: "", timeoutSec: 300, retries: 0, backoffMs: 1000, onError: "stop", strategy: "at-most-once" };
const flow: Definition = {
  schemaVersion: 2, id: "f1", name: "Review", description: "", enabled: false, allowedExecutables: [], allowedHosts: [], allowHttp: false, concurrency: 1, timeoutSec: 3600,
  trigger: { type: "manual", schedule: "", catchUp: "skip", event: "", filter: "" },
  steps: [{ ...step, id: "check1", type: "condition", params: { condition: "true" }, then: [{ ...step, id: "ask1", type: "bot.tell", params: { botId: "b1", message: "" } }], else: [] }],
};

beforeEach(() => {
  vi.clearAllMocks();
  mocks.runs.mockResolvedValue([]);
});
afterEach(cleanup);

describe("FlowBuilder", () => {
  it("shows the daemon's issues on the field of the nested block after a failed save", async () => {
    mocks.save.mockResolvedValue({ saved: false, definition: flow, issues: [{ path: "steps.0.then.0.params.message", message: "Message is required" }] });
    render(<FlowBuilder initial={flow} initialIssues={[]} types={types} topics={[]} offline={false} onBack={vi.fn()} onSaved={vi.fn()} />);
    fireEvent.change(screen.getByRole("textbox", { name: "Flow name" }), { target: { value: "Review finished work" } });
    fireEvent.click(screen.getByRole("button", { name: /^Save/ }));
    expect(await screen.findByText(/Not saved: 1 problem/)).toBeVisible();
    expect(mocks.save).toHaveBeenCalledWith({ ...flow, name: "Review finished work" });

    const card = screen.getByRole("button", { name: /Reviewer/ });
    expect(within(card.parentElement!).getByText("1 issue")).toBeVisible();
    fireEvent.click(card);
    const message = screen.getByLabelText("Message");
    expect(message).toHaveAttribute("aria-invalid", "true");
    expect(within(screen.getByRole("region", { name: "Block ask1" })).getByRole("alert")).toHaveTextContent("Message is required");
  });
});
