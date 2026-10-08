/// <reference lib="es2024.promise" />
import type { ReactNode } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { registerCommands, registerProvider } from "@/lib/commands";
import { useUI } from "@/stores/ui";
import { CommandPalette } from "./CommandPalette";

// Keep these regressions about palette orchestration, not cmdk/Base UI layout.
vi.mock("@/components/ui/dialog", () => ({
  Dialog: ({ open, children }: { open: boolean; children: ReactNode }) => open ? <div>{children}</div> : null,
  DialogPopup: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: ReactNode }) => <h1>{children}</h1>,
}));
vi.mock("cmdk", () => {
  const Container = ({ children }: { children: ReactNode }) => <div>{children}</div>;
  return { Command: Object.assign(Container, {
    Input: ({ value, onValueChange }: { value: string; onValueChange: (value: string) => void }) => <input aria-label="Search" value={value} onChange={(event) => onValueChange(event.target.value)} />,
    List: Container, Group: Container, Empty: () => null,
    Item: ({ children, onSelect }: { children: ReactNode; onSelect: () => void }) => <button onClick={onSelect}>{children}</button>,
  }) };
});

const off: (() => void)[] = [];
beforeEach(() => { useUI.setState({ paletteOpen: true }); localStorage.clear(); });
afterEach(() => { cleanup(); off.splice(0).forEach((dispose) => dispose()); });
const query = (value: string) => fireEvent.change(screen.getByLabelText("Search"), { target: { value } });

it("does not search sensitive providers from All, and hides stale providers when filtered", async () => {
  const files = vi.fn().mockResolvedValue([{ id: "file.hit", title: "File hit", category: "Files", run: vi.fn() }]);
  const mail = vi.fn().mockResolvedValue([{ id: "mail.hit", title: "Mail hit", category: "Mail", run: vi.fn() }]);
  off.push(registerProvider({ id: "files", label: "Files", search: files }), registerProvider({ id: "mail", label: "Mail", explicitOnly: true, search: mail }));
  render(<CommandPalette />); query("hit");
  await screen.findByText("File hit");
  expect(mail).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Mail" }));
  expect(screen.queryByText("File hit")).toBeNull();
  await screen.findByText("Mail hit");
  expect(files).toHaveBeenCalledTimes(1);
  query("h");
  expect(screen.queryByText("Mail hit")).toBeNull();
});

it("drops cancelled responses and explains cancellation", async () => {
  const { promise, resolve: finish } = Promise.withResolvers<{ id: string; title: string; category: string; run: () => void }[]>();
  off.push(registerProvider({ id: "slow", label: "Slow", search: () => promise }));
  render(<CommandPalette />); query("slow");
  fireEvent.click(await screen.findByRole("button", { name: "Cancel search" }));
  await act(async () => finish([{ id: "stale", title: "Stale result", category: "Slow", run: () => {} }]));
  expect(screen.queryByText("Stale result")).toBeNull();
  expect(screen.getByText(/Search cancelled/)).toBeInTheDocument();
});

it("updates registered commands while open and surfaces action failures", async () => {
  render(<CommandPalette />);
  act(() => { off.push(registerCommands([{ id: "new.command", title: "New command", category: "Actions", run: async () => { throw new Error("Permission denied"); } }])); });
  fireEvent.click(screen.getByText("New command"));
  await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("Permission denied"));
  expect(useUI.getState().paletteOpen).toBe(true);
});
