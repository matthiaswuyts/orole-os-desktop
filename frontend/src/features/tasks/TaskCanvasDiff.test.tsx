import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { Session } from "@/mygo";

const api = vi.hoisted(() => ({ diff: vi.fn() }));
vi.mock("@/mygo", () => ({ Agents: { diff: api.diff }, Shell: { openExternal: vi.fn() } }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("./TaskInteractions", () => ({ TaskApprovalCard: () => null }));
import { TaskCanvas } from "./TaskCanvas";

const session = (n: number): Session => ({ id: "s1", live: true, mode: "structured", machine: "local", updatedAt: new Date(Date.UTC(2026, 9, 8, 12, 0, n)).toISOString() }) as Session;
const empty = { diff: { files: [], truncated: false }, truncated: false, error: "" };

beforeEach(() => { api.diff.mockReset(); api.diff.mockResolvedValue(empty); });
afterEach(cleanup);

it("does not fetch the worktree diff while the canvas is closed, and not once per session update while it is open", async () => {
  const view = render(<TaskCanvas session={session(0)} items={[]}><p>Conversation</p></TaskCanvas>);
  for (let n = 1; n <= 20; n++) view.rerender(<TaskCanvas session={session(n)} items={[]}><p>Conversation</p></TaskCanvas>);
  expect(api.diff).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: /Task files/ }));
  await act(async () => { await Promise.resolve(); });
  for (let n = 21; n <= 60; n++) view.rerender(<TaskCanvas session={session(n)} items={[]}><p>Conversation</p></TaskCanvas>);
  await act(async () => { await Promise.resolve(); });
  // origin/main requested a diff for each of these 40 updates.
  expect(api.diff).toHaveBeenCalledTimes(1);
});
