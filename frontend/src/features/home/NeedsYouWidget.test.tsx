import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { Bot, Pending } from "@/mygo";

const pending = vi.hoisted(() => ({ items: [] as Pending[] }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("./useTasksStatus", () => ({ useTasksStatus: () => ({ status: "ready" }) }));
vi.mock("@/features/automations/useApprovals", () => ({
  useAutomationApprovals: () => [],
  useApprovalsStore: (select: (state: { checkedAt: number }) => unknown) => select({ checkedAt: 0 }),
}));
vi.mock("@/features/mcp/usePolicyPending", async (importOriginal) => ({
  ...await importOriginal<Record<string, unknown>>(),
  usePolicyPending: () => ({ items: pending.items, checkedAt: 1, error: null, retry: () => {} }),
}));
import { useChat } from "@/stores/chat";
import { useUI } from "@/stores/ui";
import { NeedsYouWidget } from "./NeedsYouWidget";

afterEach(cleanup);

const atlas: Bot = {
  id: "atlas", name: "Atlas", avatar: "", color: "blue", description: "", instructions: "", provider: "", model: "", reasoning: "",
  config: { tools: [], mcpServers: [], skills: [], memory: [], limits: { maxSteps: 0, maxToolCalls: 0 } }, createdAt: 1, updatedAt: 1, running: false,
};
const held = (over: Partial<Pending>): Pending => ({
  id: "p1", actor: "bot:atlas", action: "mcp.tool.call", resource: "mcp/linear/create_issue", detail: "", effect: "write", fromHint: true,
  reason: "", createdAt: 1, expiresAt: Date.now() + 60_000, canRemember: true, ...over,
});

it("lists a bot's held integration call by bot name and opens that bot's chat", () => {
  useChat.setState({ bots: [atlas], selectedBotId: null });
  useUI.setState({ view: "home" });
  pending.items = [held({})];
  render(<NeedsYouWidget widgetType="needs-you" onConfig={() => {}} onRemove={() => {}} />);
  const row = screen.getByRole("button", { name: /Use create issue/ });
  expect(within(row).getByText(/Asked by Atlas/)).toBeInTheDocument();
  fireEvent.click(row);
  expect(useChat.getState().selectedBotId).toBe("atlas");
  expect(useUI.getState().view).toBe("chat");
});
