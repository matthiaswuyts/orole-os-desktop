import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/mygo", async () => (await import("../computer/testing")).mygoMock);

import { useChat } from "@/stores/chat";
import { resetSandbox, sandbox } from "../computer/testing";
import { BotActions } from "./BotHeader";
import { bot } from "./builder/testing";

const withComputer = () => useChat.setState({ bots: [bot("b1", { config: { ...bot("x").config, computer: true } })], selectedBotId: "b1", pane: null });
const computer = (state: string) => {
  sandbox.availability.mockResolvedValue({ ready: true });
  sandbox.status.mockResolvedValue({ botId: "b1", state, takenOver: false });
};

beforeEach(resetSandbox);
afterEach(() => { cleanup(); useChat.setState({ bots: [], selectedBotId: null, pane: null, settingsOpen: false }); });

describe("bot title bar actions", () => {
  it("opens and closes the Computer pane while the computer runs", async () => {
    withComputer();
    computer("running");
    render(<BotActions />);
    const button = screen.getByRole("button", { name: "Show computer" });
    await waitFor(() => expect(sandbox.status).toHaveBeenCalled());
    await waitFor(() => expect(button).not.toHaveAttribute("aria-disabled"));
    fireEvent.click(button);
    expect(useChat.getState().pane).toEqual({ kind: "computer", id: "b1" });
    fireEvent.click(screen.getByRole("button", { name: "Hide computer" }));
    expect(useChat.getState().pane).toBeNull();
  });

  it("does not open a pane that would only say the computer is off", async () => {
    withComputer();
    computer("off");
    render(<BotActions />);
    const button = screen.getByRole("button", { name: "Show computer" });
    await waitFor(() => expect(button).toHaveAttribute("aria-disabled", "true"));
    fireEvent.click(button);
    expect(useChat.getState().pane).toBeNull();
  });

  it("has no Computer button for other bots", () => {
    useChat.setState({ bots: [bot("b2")], selectedBotId: "b2", pane: null });
    render(<BotActions />);
    expect(screen.queryByRole("button", { name: "Show computer" })).toBeNull();
    expect(screen.getByRole("button", { name: "Show bot settings" })).toBeInTheDocument();
  });
});
