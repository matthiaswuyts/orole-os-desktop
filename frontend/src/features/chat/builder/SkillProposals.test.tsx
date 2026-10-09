import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { SkillProposal } from "@/mygo";

vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", async () => (await import("./testing")).mygoMock);

import { SkillProposals } from "./SkillProposals";
import { bridge, emit, resetBridge } from "./testing";

const proposal = (id: string, over: Partial<SkillProposal> = {}): SkillProposal => ({
  id, botId: "b1", name: `skill-${id}`, description: `Does ${id}`, body: `---\nname: skill-${id}\n---\nStep one`, status: "pending", createdAt: 1, ...over,
});

beforeEach(resetBridge);
afterEach(cleanup);

describe("skill proposals", () => {
  it("says nothing is active until approved and lists only pending proposals with their text", async () => {
    bridge.chat.skillProposals.mockResolvedValue([proposal("a"), proposal("old", { status: "approved" })]);
    render(<SkillProposals botId="b1" />);
    expect(await screen.findByText("skill-a")).toBeInTheDocument();
    expect(bridge.chat.skillProposals).toHaveBeenCalledWith("b1");
    expect(screen.queryByText("skill-old")).toBeNull();
    expect(screen.getByText(/Nothing is active until you approve it/)).toBeInTheDocument();
    expect(screen.getByLabelText("Proposed skill text")).toHaveTextContent("Step one");
  });

  it("approves and rejects through the daemon, reloading afterwards", async () => {
    bridge.chat.skillProposals.mockResolvedValueOnce([proposal("a"), proposal("b")]).mockResolvedValue([]);
    bridge.chat.resolveSkill.mockResolvedValue(undefined);
    render(<SkillProposals botId="b1" />);
    fireEvent.click(await screen.findByRole("button", { name: "Approve skill skill-a" }));
    await waitFor(() => expect(bridge.chat.resolveSkill).toHaveBeenCalledWith({ id: "a", approve: true }));
    expect(await screen.findByText("No skills are waiting for your approval.")).toBeInTheDocument();
  });

  it("rejects with approve=false", async () => {
    bridge.chat.skillProposals.mockResolvedValue([proposal("b")]);
    bridge.chat.resolveSkill.mockResolvedValue(undefined);
    render(<SkillProposals botId="b1" />);
    fireEvent.click(await screen.findByRole("button", { name: "Reject skill skill-b" }));
    await waitFor(() => expect(bridge.chat.resolveSkill).toHaveBeenCalledWith({ id: "b", approve: false }));
  });

  it("shows the daemon's refusal and keeps the proposal", async () => {
    bridge.chat.skillProposals.mockResolvedValue([proposal("a")]);
    bridge.chat.resolveSkill.mockRejectedValue(new Error("bad_request: a skill with that name already exists"));
    render(<SkillProposals botId="b1" />);
    fireEvent.click(await screen.findByRole("button", { name: "Approve skill skill-a" }));
    expect(await screen.findByText("a skill with that name already exists")).toBeInTheDocument();
    expect(screen.getByText("skill-a")).toBeInTheDocument();
  });

  it("folds a long body until asked", async () => {
    const body = Array.from({ length: 30 }, (_, i) => `line ${i}`).join("\n");
    bridge.chat.skillProposals.mockResolvedValue([proposal("a", { body })]);
    render(<SkillProposals botId="b1" />);
    await screen.findByText("skill-a");
    expect(screen.queryByText("line 29")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Show full text" }));
    expect(screen.getByText("line 29")).toBeInTheDocument();
  });

  it("refreshes on memory events", async () => {
    bridge.chat.skillProposals.mockResolvedValueOnce([]).mockResolvedValue([proposal("fresh")]);
    render(<SkillProposals botId="b1" />);
    await screen.findByText("No skills are waiting for your approval.");
    emit("chatMemory", { scope: "" });
    expect(await screen.findByText("skill-fresh")).toBeInTheDocument();
  });
});
