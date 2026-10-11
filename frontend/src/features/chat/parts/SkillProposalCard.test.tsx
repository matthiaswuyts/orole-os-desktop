import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { SkillProposal } from "@/mygo";

vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", async () => (await import("../builder/testing")).mygoMock);

import { bot, bridge, resetBridge } from "../builder/testing";
import { useChat } from "@/stores/chat";
import { StructuredPart } from "./StructuredPart";

const spec = { proposalId: "p1", botId: "b1", name: "trip-planner", description: "Plans trips step by step", body: "---\nname: trip-planner\n---\nAsk for the dates first.", updates: false };
const part = (over: Partial<typeof spec> = {}) => ({ name: "skill", spec: JSON.stringify({ ...spec, ...over }) });
const proposal = (status: string): SkillProposal => ({ id: "p1", botId: "b1", name: spec.name, description: spec.description, body: spec.body, status, createdAt: 1 });

beforeEach(() => {
  resetBridge();
  bridge.chat.skillProposal.mockResolvedValue(proposal("pending"));
  bridge.chat.bots.mockResolvedValue([bot("b1", { name: "Atlas" })]);
  useChat.setState({ bots: [bot("b1", { name: "Atlas" })], available: true });
});
afterEach(cleanup);

describe("skill proposal card", () => {
  it("shows the draft and asks the daemon for its live state", async () => {
    render(<StructuredPart part={part()} />);
    expect(screen.getByText("trip-planner")).toBeInTheDocument();
    expect(screen.getByText("Plans trips step by step")).toBeInTheDocument();
    expect(screen.getByText("New skill")).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: "Add skill" })).toBeEnabled();
    expect(bridge.chat.skillProposal).toHaveBeenCalledWith("p1");
    expect(screen.getByRole("button", { name: "Dismiss" })).toBeEnabled();
  });

  it("labels a draft that updates an earlier skill", async () => {
    render(<StructuredPart part={part({ updates: true })} />);
    expect(await screen.findByText("Update")).toBeInTheDocument();
    expect(screen.queryByText("New skill")).toBeNull();
  });

  it("adds the skill, says the bot can use it and refreshes the bots", async () => {
    let finish: () => void = () => {};
    bridge.chat.resolveSkill.mockReturnValue(new Promise<void>((resolve) => { finish = resolve; }));
    bridge.chat.bots.mockResolvedValue([bot("b1", { name: "Atlas", config: { tools: [], mcpServers: [], skills: ["trip-planner"], memory: [], limits: { maxSteps: 0, maxToolCalls: 0 } } })]);
    render(<StructuredPart part={part()} />);
    fireEvent.click(await screen.findByRole("button", { name: "Add skill" }));
    expect(bridge.chat.resolveSkill).toHaveBeenCalledWith({ id: "p1", approve: true });
    await waitFor(() => expect(screen.getByRole("button", { name: "Dismiss" })).toBeDisabled());
    expect(screen.getByRole("button", { name: /^Add skill/ })).toBeDisabled();
    finish();
    expect(await screen.findByText("Added. Atlas can use it now.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add skill" })).toBeNull();
    await waitFor(() => expect(useChat.getState().bots[0]!.config.skills).toEqual(["trip-planner"]));
    expect(bridge.chat.bots).toHaveBeenCalled();
  });

  it("falls back to 'This bot' when the bot is not in the store", async () => {
    useChat.setState({ bots: [] });
    bridge.chat.skillProposal.mockResolvedValue(proposal("approved"));
    render(<StructuredPart part={part()} />);
    expect(await screen.findByText("Added. This bot can use it now.")).toBeInTheDocument();
  });

  it("dismisses without touching the bots", async () => {
    bridge.chat.resolveSkill.mockResolvedValue(undefined);
    render(<StructuredPart part={part()} />);
    fireEvent.click(await screen.findByRole("button", { name: "Dismiss" }));
    expect(bridge.chat.resolveSkill).toHaveBeenCalledWith({ id: "p1", approve: false });
    expect(await screen.findByText("Dismissed")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add skill" })).toBeNull();
    expect(bridge.chat.bots).not.toHaveBeenCalled();
  });

  it("shows an already dismissed draft as dismissed", async () => {
    bridge.chat.skillProposal.mockResolvedValue(proposal("rejected"));
    render(<StructuredPart part={part()} />);
    expect(await screen.findByText("Dismissed")).toBeInTheDocument();
  });

  it("says when a newer draft replaced this one", async () => {
    bridge.chat.skillProposal.mockRejectedValue(new Error("not_found: skill proposal not found"));
    render(<StructuredPart part={part()} />);
    expect(await screen.findByText("Replaced by a newer draft")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add skill" })).toBeNull();
  });

  it("shows another load failure inline and retries", async () => {
    bridge.chat.skillProposal.mockRejectedValueOnce(new Error("internal: daemon is down")).mockResolvedValue(proposal("pending"));
    render(<StructuredPart part={part()} />);
    expect(await screen.findByRole("alert")).toHaveTextContent("daemon is down");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByRole("button", { name: "Add skill" })).toBeEnabled();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("keeps Add retryable after the daemon refuses", async () => {
    bridge.chat.resolveSkill.mockRejectedValueOnce(new Error("bad_request: a skill named trip-planner already exists and was not created by you")).mockResolvedValue(undefined);
    render(<StructuredPart part={part()} />);
    fireEvent.click(await screen.findByRole("button", { name: "Add skill" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/^a skill named trip-planner already exists and was not created by you$/);
    expect(screen.getByRole("button", { name: "Add skill" })).toBeEnabled();
    expect(bridge.chat.bots).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Add skill" }));
    expect(await screen.findByText("Added. Atlas can use it now.")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(bridge.chat.resolveSkill).toHaveBeenCalledTimes(2);
  });

  it("keeps the instructions collapsed until asked and shows them verbatim", async () => {
    render(<StructuredPart part={part()} />);
    await screen.findByRole("button", { name: "Add skill" });
    expect(screen.queryByLabelText("Skill instructions")).toBeNull();
    const toggle = screen.getByRole("button", { name: "View instructions" });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    fireEvent.click(toggle);
    expect((await screen.findByLabelText("Skill instructions")).textContent).toBe(spec.body);
    expect(toggle).toHaveAttribute("aria-expanded", "true");
    fireEvent.click(toggle);
    await waitFor(() => expect(screen.queryByLabelText("Skill instructions")).toBeNull());
  });
});

describe("structured part kinds", () => {
  it("renders kind skill in the structured wrapper", async () => {
    const { container } = render(<StructuredPart part={part()} />);
    await screen.findByRole("button", { name: "Add skill" });
    expect(container.querySelector("[data-slot=chat-structured][data-kind=skill] [data-slot=skill-card]")).not.toBeNull();
  });

  it("still ignores unknown kinds and malformed specs", () => {
    const { container } = render(<><StructuredPart part={{ name: "bogus", spec: "{}" }} /><StructuredPart part={{ name: "skill", spec: "{nope" }} /></>);
    expect(container.querySelector("[data-slot=chat-structured]")).toBeNull();
  });
});
