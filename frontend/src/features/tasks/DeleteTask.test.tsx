import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { Session } from "@/mygo";

const api = vi.hoisted(() => ({ stop: vi.fn(), remove: vi.fn() }));
vi.mock("@/mygo", () => ({ Agents: api, Projects: {}, events: {} }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
import { useAgents } from "@/stores/agents";
import { useChat } from "@/stores/chat";
import { TooltipProvider } from "@/components/ui/tooltip";
import { DeleteTaskButton } from "./DeleteTask";

const session = (id: string, patch: Partial<Session> = {}) => ({ id, title: `Task ${id}`, task: "", kind: "claude", live: false, status: "done", updatedAt: "2026-10-12T10:00:00Z", ...patch }) as Session;
const mount = (s: Session) => render(<TooltipProvider><DeleteTaskButton session={s} /></TooltipProvider>);
const order: string[] = [];
// The dialog's confirm button is the only element with the visible text "Delete task"; the icon button is labelled by aria-label.
const confirm = async () => { fireEvent.click(await screen.findByText("Delete task")); };

beforeEach(() => {
  vi.clearAllMocks(); order.length = 0;
  api.stop.mockImplementation(async () => { order.push("stop"); });
  api.remove.mockImplementation(async () => { order.push("remove"); });
  useAgents.setState({ sessions: { a: session("a"), b: session("b") }, openId: "a" });
  useChat.setState({ pane: null });
});
afterEach(cleanup);

it("asks first, naming what is removed, and deletes nothing on cancel", async () => {
  mount(session("a"));
  fireEvent.click(screen.getByRole("button", { name: "Delete task" }));
  expect(await screen.findByText(/The task and its history/)).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByText(/The task and its history/)).toBeNull());
  expect(api.remove).not.toHaveBeenCalled();
  expect(useAgents.getState().sessions.a).toBeDefined(); expect(useAgents.getState().openId).toBe("a");
});

it("removes the task from the list and closes it when it was open", async () => {
  mount(session("a"));
  fireEvent.click(screen.getByRole("button", { name: "Delete task" }));
  await confirm();
  await waitFor(() => expect(api.remove).toHaveBeenCalledExactlyOnceWith("a"));
  expect(api.stop).not.toHaveBeenCalled();
  expect(useAgents.getState().sessions).toEqual({ b: expect.anything() });
  expect(useAgents.getState().openId).toBeNull();
});

it("keeps another open task selected when a different one is deleted", async () => {
  useAgents.setState({ openId: "b" });
  mount(session("a"));
  fireEvent.click(screen.getByRole("button", { name: "Delete task" }));
  await confirm();
  await waitFor(() => expect(api.remove).toHaveBeenCalled());
  expect(useAgents.getState().openId).toBe("b");
});

it("stops a running task before deleting it and closes its bot-chat pane", async () => {
  useChat.getState().openTaskPane("a");
  mount(session("a", { live: true, status: "working" }));
  fireEvent.click(screen.getByRole("button", { name: "Delete task" }));
  expect(await screen.findByText(/It will be stopped/)).toBeTruthy();
  await confirm();
  await waitFor(() => expect(api.remove).toHaveBeenCalled());
  expect(order).toEqual(["stop", "remove"]);
  expect(useChat.getState().pane).toBeNull();
});

it("keeps the task and shows the error when the daemon refuses", async () => {
  api.remove.mockRejectedValueOnce(new Error("conflict: stop the agent before removing it"));
  mount(session("a"));
  fireEvent.click(screen.getByRole("button", { name: "Delete task" }));
  await confirm();
  expect((await screen.findByRole("alert")).textContent).toContain("stop the agent before removing it");
  expect(useAgents.getState().sessions.a).toBeDefined();
});
