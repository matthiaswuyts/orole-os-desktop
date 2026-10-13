import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Proposal } from "@/mygo";

vi.mock("@/lib/backend", () => ({ inDesktop: true }));
// vi.mock is hoisted above static imports, so the shared bridge mock must be loaded dynamically (see builder/testing.ts).
vi.mock("@/mygo", async () => (await import("../builder/testing")).mygoMock);

import { bot, bridge, resetBridge } from "../builder/testing";
import { useChat } from "@/stores/chat";
import { useUI } from "@/stores/ui";
import { StructuredPart } from "./StructuredPart";

const part = { name: "automation", spec: JSON.stringify({ proposalId: "p1", botId: "b1", name: "Watch CI" }) };

const proposal = (over: Partial<Proposal> = {}): Proposal => ({
  id: "p1", botId: "b1", replaces: "", replacesEnabled: false, rationale: "Tells you when CI on main fails.", risks: [], status: "pending", automationId: "", createdAt: 1, decidedAt: 0,
  definition: {
    schemaVersion: 2, id: "", name: "Watch CI", description: "", enabled: false,
    trigger: { type: "watch", schedule: "", catchUp: "skip", event: "", filter: "", watch: { source: "github", kind: "workflow_failed", repo: "acme/app", branch: "main" } },
    steps: [{ id: "tell", name: "", type: "bot.tell", params: { botId: "b1", message: "CI failed" }, when: "", timeoutSec: 0, retries: 0, backoffMs: 0, onError: "", strategy: "" }],
    allowedExecutables: [], allowedHosts: [], allowHttp: false, concurrency: 0, timeoutSec: 3600,
  },
  ...over,
});

beforeEach(() => {
  resetBridge();
  bridge.automations.proposal.mockResolvedValue(proposal());
  bridge.automations.stepTypes.mockResolvedValue([{ type: "bot.tell", label: "Ask a bot", group: "Bots", description: "", available: true, reason: "", sideEffect: true, keySupported: false, fields: [] }]);
  useChat.setState({ bots: [bot("b1", { name: "Atlas" })], available: true });
});
afterEach(cleanup);

describe("automation proposal card", () => {
  it("shows the daemon's stored draft: trigger, blocks and why", async () => {
    render(<StructuredPart part={part} />);
    expect(await screen.findByText("On failed workflows in acme/app (main)")).toBeInTheDocument();
    expect(screen.getByText("Ask a bot")).toBeInTheDocument();
    expect(screen.getByText(/Atlas · CI failed/)).toBeInTheDocument();
    expect(screen.getByText("Tells you when CI on main fails.")).toBeInTheDocument();
    expect(bridge.automations.proposal).toHaveBeenCalledWith("p1");
  });

  it("turns a safe draft on and opens it from the card", async () => {
    bridge.automations.resolveProposal.mockResolvedValue(proposal({ status: "approved", automationId: "a1", definition: { ...proposal().definition, id: "a1", enabled: true } }));
    render(<StructuredPart part={part} />);
    fireEvent.click(await screen.findByRole("button", { name: "Turn on" }));
    expect(bridge.automations.resolveProposal).toHaveBeenCalledWith({ id: "p1", approve: true, enable: true });
    expect(await screen.findByText("Turned on.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open in Automations" }));
    expect(useUI.getState()).toMatchObject({ view: "automations", automationFocus: "a1" });
  });

  it("offers only Add turned off for a draft that runs programs, and lists what it runs", async () => {
    bridge.automations.proposal.mockResolvedValue(proposal({ risks: ["Allows running go", "Runs go test ./... in C:\\app"] }));
    bridge.automations.resolveProposal.mockResolvedValue(proposal({ status: "approved", automationId: "a1" }));
    render(<StructuredPart part={part} />);
    expect(await screen.findByText("Runs go test ./... in C:\\app")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Turn on" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Add turned off" }));
    expect(bridge.automations.resolveProposal).toHaveBeenCalledWith({ id: "p1", approve: true, enable: false });
    expect(await screen.findByText(/Added turned off/)).toBeInTheDocument();
  });

  it("keeps an edited automation's switch unless the edit needs review", async () => {
    bridge.automations.proposal.mockResolvedValue(proposal({ replaces: "a1", replacesEnabled: true }));
    bridge.automations.resolveProposal.mockResolvedValue(proposal({ replaces: "a1", status: "approved", automationId: "a1" }));
    render(<StructuredPart part={part} />);
    expect(await screen.findByText("Update")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Apply changes" }));
    expect(bridge.automations.resolveProposal).toHaveBeenLastCalledWith({ id: "p1", approve: true, enable: true });
    cleanup();

    bridge.automations.proposal.mockResolvedValue(proposal({ replaces: "a1", replacesEnabled: true, risks: ["Sends POST https://x.example"] }));
    render(<StructuredPart part={part} />);
    fireEvent.click(await screen.findByRole("button", { name: "Apply changes" }));
    expect(bridge.automations.resolveProposal).toHaveBeenLastCalledWith({ id: "p1", approve: true, enable: false });
  });

  it("shows the daemon's refusal and keeps the decision open", async () => {
    bridge.automations.resolveProposal.mockRejectedValueOnce(new Error("conflict: the automation changed after this draft was made; ask the bot for a new draft"));
    render(<StructuredPart part={part} />);
    fireEvent.click(await screen.findByRole("button", { name: "Turn on" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/^the automation changed after this draft was made; ask the bot for a new draft$/);
    expect(screen.getByRole("button", { name: "Turn on" })).toBeEnabled();
  });

  it("shows a replaced or dismissed draft without actions", async () => {
    bridge.automations.proposal.mockResolvedValue(proposal({ status: "replaced" }));
    render(<StructuredPart part={part} />);
    expect(await screen.findByText("Replaced by a newer draft")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Turn on" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Dismiss" })).toBeNull();
  });
});
