import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Pending } from "@/mygo";

const bridge = vi.hoisted(() => ({
  pending: vi.fn(), resolve: vi.fn(),
  listener: null as ((event: { items: Pending[] }) => void) | null,
}));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({
  Policy: { pending: bridge.pending, resolve: bridge.resolve },
  events: {
    policyPending: { on: (listener: (event: { items: Pending[] }) => void) => { bridge.listener = listener; return () => { bridge.listener = null; }; } },
    daemonState: { on: () => () => {} },
  },
}));
import { PolicyApprovals } from "./PolicyApprovals";

const request: Pending = {
  id: "test-approval", actor: "user", action: "mail.send", resource: "mail/test",
  detail: "Send a message", effect: "write", fromHint: false, reason: "Requires approval",
  createdAt: Date.now(), expiresAt: Date.now() + 300_000, canRemember: true,
};

beforeEach(() => {
  bridge.pending.mockReset(); bridge.resolve.mockReset();
  bridge.resolve.mockResolvedValue(undefined);
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("policy approval queue", () => {
  it("fetches pending approvals on mount and resolves a remembered allow", async () => {
    bridge.pending.mockResolvedValueOnce([request]).mockResolvedValue([]);
    render(<PolicyApprovals />);
    const remember = await screen.findByRole("checkbox");
    fireEvent.click(remember);
    fireEvent.click(screen.getByRole("button", { name: "Allow" }));
    await waitFor(() => expect(bridge.resolve).toHaveBeenCalledWith(request.id, true, true));
  });

  it("never offers remember for destructive actions and sends false on deny", async () => {
    bridge.pending.mockResolvedValueOnce([{ ...request, effect: "destructive", canRemember: false }]).mockResolvedValue([]);
    render(<PolicyApprovals />);
    const deny = await screen.findByRole("button", { name: "Deny" });
    expect(screen.queryByRole("checkbox")).toBeNull();
    fireEvent.click(deny);
    await waitFor(() => expect(bridge.resolve).toHaveBeenCalledWith(request.id, false, false));
  });

  it("does not let a late initial snapshot overwrite a newer pending event", async () => {
    let finish: (items: Pending[]) => void = () => { throw new Error("Pending snapshot was not requested"); };
    bridge.pending.mockImplementation(() => new Promise<Pending[]>((resolve) => { finish = resolve; }));
    render(<PolicyApprovals />);
    act(() => bridge.listener?.({ items: [request] }));
    expect(await screen.findByText("Send a message")).toBeInTheDocument();
    await act(async () => finish([]));
    expect(screen.getByRole("button", { name: "Allow" })).toBeInTheDocument();
  });
});
