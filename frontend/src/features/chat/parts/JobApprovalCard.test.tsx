import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Pending } from "@/mygo";

const bridge = vi.hoisted(() => ({
  pending: vi.fn(), resolve: vi.fn(), addRule: vi.fn(),
  listener: null as ((event: { items: Pending[] }) => void) | null,
}));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({
  Policy: { pending: bridge.pending, resolve: bridge.resolve, addRule: bridge.addRule },
  events: {
    policyPending: { on: (listener: (event: { items: Pending[] }) => void) => { bridge.listener = listener; return () => { bridge.listener = null; }; } },
    daemonState: { on: () => () => {} },
  },
}));
import { useChat } from "@/stores/chat";
import { BotApprovals, JobApprovalCard } from "./JobApprovalCard";

const request: Pending = {
  id: "job-1", actor: "bot:atlas", action: "agent.start", resource: "project:demo",
  detail: "Fix the flaky login test in demo", effect: "write", fromHint: false, reason: "Bots need an approval to start tasks.",
  createdAt: Date.now(), expiresAt: Date.now() + 300_000, canRemember: true,
};
const rule = (action: string) => ({ id: 0, actor: "bot:atlas", action, resource: "*", maxEffect: "write", decision: "allow", note: "always allow from chat", createdAt: 0 });

beforeEach(() => {
  bridge.pending.mockReset(); bridge.resolve.mockReset(); bridge.addRule.mockReset();
  bridge.resolve.mockResolvedValue(undefined);
  bridge.addRule.mockImplementation((r: unknown) => Promise.resolve(r));
});
afterEach(cleanup);

describe("job approval card", () => {
  it("allows once with the daemon's resolve and keeps the receipt", async () => {
    render(<JobApprovalCard request={request} requester="Atlas" />);
    expect(screen.getByText("Start a code task")).toBeInTheDocument();
    expect(screen.getByText("Fix the flaky login test in demo")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Allow once" }));
    await waitFor(() => expect(bridge.resolve).toHaveBeenCalledWith("job-1", true, false));
    expect(bridge.addRule).not.toHaveBeenCalled();
    expect(await screen.findByRole("status")).toHaveTextContent("Allowed once");
    expect(screen.queryByRole("button", { name: "Deny" })).toBeNull();
  });

  it("always: remembers both task actions for this bot before resolving", async () => {
    render(<JobApprovalCard request={{ ...request, action: "agent.prompt" }} requester="Atlas" />);
    expect(screen.getByText("Send a follow-up to a task")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Always" }));
    await waitFor(() => expect(bridge.resolve).toHaveBeenCalledWith("job-1", true, false));
    expect(bridge.addRule).toHaveBeenCalledTimes(2);
    expect(bridge.addRule).toHaveBeenCalledWith(rule("agent.start"));
    expect(bridge.addRule).toHaveBeenCalledWith(rule("agent.prompt"));
    expect(bridge.addRule.mock.invocationCallOrder[1]).toBeLessThan(bridge.resolve.mock.invocationCallOrder[0]!);
    expect(await screen.findByRole("status")).toHaveTextContent("Always allowed");
  });

  it("denies without a rule and offers no Always when the daemon cannot remember", async () => {
    render(<JobApprovalCard request={{ ...request, canRemember: false }} requester="Atlas" />);
    expect(screen.queryByRole("button", { name: "Always" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Deny" }));
    await waitFor(() => expect(bridge.resolve).toHaveBeenCalledWith("job-1", false, false));
    expect(bridge.addRule).not.toHaveBeenCalled();
    expect(await screen.findByRole("status")).toHaveTextContent("Denied");
  });

  it("shows a failed decision and keeps the buttons", async () => {
    bridge.resolve.mockRejectedValue(new Error("daemon offline"));
    render(<JobApprovalCard request={request} requester="Atlas" />);
    fireEvent.click(screen.getByRole("button", { name: "Allow once" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("daemon offline");
    expect(screen.getByRole("button", { name: "Allow once" })).toBeEnabled();
  });
});

describe("bot approvals", () => {
  it("lists only this bot's held requests, named after the bot, and drops them when the queue does", async () => {
    useChat.setState({ bots: [{ id: "atlas", name: "Atlas", avatar: "", color: "blue", description: "", instructions: "", provider: "", model: "", reasoning: "", config: { tools: [], mcpServers: [], skills: [], memory: [], limits: { maxSteps: 0, maxToolCalls: 0 } }, createdAt: 1, updatedAt: 1, running: false }] });
    bridge.pending.mockResolvedValue([request, { ...request, id: "other", actor: "bot:coach" }, { ...request, id: "mcp", actor: "user", action: "mail.send" }]);
    render(<BotApprovals botId="atlas" />);
    expect(await screen.findByRole("region", { name: "Approvals for Atlas" })).toBeInTheDocument();
    expect(screen.getAllByRole("group")).toHaveLength(1);
    expect(screen.getByText("requested by Atlas")).toBeInTheDocument();
    act(() => bridge.listener?.({ items: [{ ...request, id: "other", actor: "bot:coach" }] }));
    expect(screen.queryByRole("region")).toBeNull();
  });
});
