import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useUI } from "@/stores/ui";
import { TerminalsView } from "./TerminalsView";

const api = vi.hoisted(() => ({ list: vi.fn(), shells: vi.fn(), create: vi.fn() }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({ Terminals: api, events: { terminalChanged: { on: () => () => {} }, terminalRemoved: { on: () => () => {} } } }));
vi.mock("@/stores/settings", () => ({ useSettings: (select: (state: { terminal: { fontSize: number } }) => unknown) => select({ terminal: { fontSize: 14 } }) }));
vi.mock("./TerminalPane", () => ({ TerminalPane: ({ id }: { id: string }) => <div data-testid="terminal-pane">{id}</div> }));
beforeEach(() => { vi.clearAllMocks(); api.shells.mockResolvedValue([]); useUI.setState({ pendingTerminal: null }); });
afterEach(cleanup);

it("opens a palette-selected exited terminal on its first mount without creating a shell", async () => {
  api.list.mockResolvedValue([{ id: "exited", title: "Finished shell", state: "exited", startedAt: "2026-01-01", exitCode: 0 }]);
  useUI.getState().openTerminal("exited");
  render(<TerminalsView />);
  expect(await screen.findByTestId("terminal-pane")).toHaveTextContent("exited");
  expect(api.create).not.toHaveBeenCalled();
  expect(useUI.getState().pendingTerminal).toBeNull();
});

it("reports a removed search target instead of silently creating a replacement", async () => {
  api.list.mockResolvedValue([]);
  useUI.getState().openTerminal("removed");
  render(<TerminalsView />);
  await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("no longer exists"));
  expect(api.create).not.toHaveBeenCalled();
  expect(screen.queryByTestId("terminal-pane")).toBeNull();
});
