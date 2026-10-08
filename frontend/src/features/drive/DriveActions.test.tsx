import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Preview } from "@/mygo";

const bridge = vi.hoisted(() => ({ recipients: vi.fn(), share: vi.fn(), trash: vi.fn() }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({
  Drive: bridge,
  events: {
    mcpStatus: { on: () => () => {} },
    mcpAuthDone: { on: () => () => {} },
    daemonState: { on: () => () => {} },
  },
}));
import { DriveActions } from "./DriveActions";

const file = { id: "file-1", name: "Selected note" };
const preview: Preview = { fileId: file.id, serverId: "server-1", snapshotHash: "a".repeat(64), kind: "text", mimeType: "text/plain", text: "Exact reviewed text", dataUrl: "", truncated: false };
beforeEach(() => {
  bridge.recipients.mockReset().mockResolvedValue([]);
  bridge.share.mockReset().mockResolvedValue({ sessionId: "session-1", snapshotHash: preview.snapshotHash });
  bridge.trash.mockReset().mockResolvedValue({ id: file.id, confirmed: true });
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("selected Drive file actions", () => {
  it("does not offer sharing or trash when discovered capabilities do not support them", () => {
    render(<DriveActions file={file} preview={null} canTrash={false} trashed={vi.fn()} />);
    expect(screen.getByText(/Sharing requires a complete text/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Move to trash/ })).toBeNull();
    expect(bridge.recipients).not.toHaveBeenCalled();
    expect(bridge.share).not.toHaveBeenCalled();
  });

  it("provides an honest no-agent state", async () => {
    render(<DriveActions file={file} preview={preview} canTrash={false} trashed={vi.fn()} />);
    expect(await screen.findByText(/No eligible agent sessions/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Open agents" })).toBeInTheDocument();
  });

  it("trashes only after explicit confirmation and server confirmation", async () => {
    const trashed = vi.fn();
    render(<DriveActions file={file} preview={null} canTrash trashed={trashed} />);
    fireEvent.click(screen.getByRole("button", { name: "Move to trash…" }));
    expect(bridge.trash).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(bridge.trash).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Move to trash…" }));
    fireEvent.click(screen.getByRole("button", { name: "Move to trash" }));
    await waitFor(() => expect(bridge.trash).toHaveBeenCalledWith(file.id));
    await waitFor(() => expect(trashed).toHaveBeenCalledTimes(1));
  });

  it("reviews exact content and sends only identifiers through the bound handoff", async () => {
    bridge.recipients.mockResolvedValue([{ id: "session-1", title: "Review notes", kind: "claude", model: "model-1", live: true, revision: "b".repeat(64) }]);
    render(<DriveActions file={file} preview={preview} canTrash={false} trashed={vi.fn()} />);
    fireEvent.click(await screen.findByRole("combobox", { name: "Recipient agent" }));
    fireEvent.click(await screen.findByRole("option", { name: /Review notes/ }));
    fireEvent.change(screen.getByLabelText("What should the agent do?"), { target: { value: "Summarize" } });
    fireEvent.click(screen.getByRole("button", { name: "Review snapshot" }));
    expect(await screen.findByText(preview.text)).toBeInTheDocument();
    expect(screen.getByText(/Expiry prevents an unsent handoff/)).toBeInTheDocument();
    expect(bridge.share).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Send snapshot once" }));
    await waitFor(() => expect(bridge.share).toHaveBeenCalledWith({ serverId: "server-1", fileId: file.id, snapshotHash: preview.snapshotHash, sessionId: "session-1", recipientRevision: "b".repeat(64), task: "Summarize" }));
    expect(await screen.findByText(/Snapshot sent once/)).toBeInTheDocument();
  });
});
