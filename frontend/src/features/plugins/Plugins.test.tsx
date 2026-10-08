import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { PluginRecord, PluginRunResult } from "@/mygo";

const bridge = vi.hoisted(() => ({
  list: vi.fn(), run: vi.fn(),
  changed: null as ((event: { id: string }) => void) | null,
}));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({
  Plugins: { list: bridge.list, run: bridge.run },
  events: { pluginsChanged: { on: (listener: (event: { id: string }) => void) => { bridge.changed = listener; return () => { bridge.changed = null; }; } } },
}));
import { usePlugins } from "@/stores/plugins";
import { useDaemon } from "@/stores/daemon";
import { PluginActionView } from "./PluginActionView";
import { PluginSettings } from "./PluginSettings";
import { parseManifest } from "./manifest";

const record: PluginRecord = {
  manifest: {
    schemaVersion: 1, id: "test.status", name: "Test status", version: "1", description: "A regression fixture",
    capabilities: [{ serverId: "server-one", tool: "status" }],
    actions: [{ id: "status", title: "Read status", description: "Read current status", serverId: "server-one", tool: "status", arguments: {} }],
    commands: [{ id: "status", title: "Read status", description: "", actionId: "status" }],
    widgets: [],
  },
  digest: "digest-one", approvedDigest: "digest-one", enabled: true, updatedAt: 1,
};
let stopSync: (() => void) | undefined;

beforeEach(() => {
  bridge.list.mockReset(); bridge.run.mockReset();
  bridge.list.mockResolvedValue([record]);
  useDaemon.setState({ link: "connected" });
  usePlugins.setState({ records: [record], staleIds: [], ready: true, loading: false, error: null });
});
afterEach(() => { cleanup(); stopSync?.(); stopSync = undefined; });

describe("declarative plugin actions", () => {
  it("does not invoke on mount, and displays user-triggered output as bounded inert text", async () => {
    const payload = '<img src=x onerror="alert(1)">\u001b\u202e' + "x".repeat(140_000);
    bridge.run.mockResolvedValue({ text: payload, isError: false, truncated: false });
    const { container } = render(<PluginActionView record={record} action={record.manifest.actions[0]!} />);
    expect(bridge.run).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Run action" }));
    const output = await screen.findByLabelText("Plugin tool result");
    expect(output.textContent).toContain('<img src=x onerror="alert(1)">');
    expect(output.textContent!.length).toBeLessThanOrEqual(128 * 1024);
    expect(output.textContent).not.toContain("\u001b");
    expect(output.textContent).not.toContain("\u202e");
    expect(container.querySelector("img")).toBeNull();
    expect(screen.getByText(/Output was truncated/)).toBeInTheDocument();
  });

  it("discards a result if trust was revoked while the request was running", async () => {
    let finish!: (result: PluginRunResult) => void;
    bridge.run.mockImplementation(() => new Promise<PluginRunResult>((resolve) => { finish = resolve; }));
    render(<PluginActionView record={record} action={record.manifest.actions[0]!} />);
    fireEvent.click(screen.getByRole("button", { name: "Run action" }));
    act(() => usePlugins.setState({ records: [{ ...record, enabled: false, approvedDigest: "" }], staleIds: [record.manifest.id] }));
    await act(async () => finish({ text: "A stale result", isError: false, truncated: false }));
    expect(screen.queryByLabelText("Plugin tool result")).toBeNull();
    expect(screen.getByText(/result was discarded/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Run action" })).toBeDisabled();
  });
});

describe("plugin synchronization", () => {
  it("does not restore stale trust from an initial snapshot after a newer change", async () => {
    let finishInitial!: (records: PluginRecord[]) => void;
    bridge.list.mockImplementationOnce(() => new Promise<PluginRecord[]>((resolve) => { finishInitial = resolve; }));
    bridge.list.mockResolvedValue([{ ...record, enabled: false, approvedDigest: "" }]);
    stopSync = usePlugins.getState().startSync();
    bridge.changed?.({ id: record.manifest.id });
    expect(usePlugins.getState().staleIds).toContain(record.manifest.id);
    await waitFor(() => expect(usePlugins.getState().records[0]?.enabled).toBe(false));
    await act(async () => finishInitial([record]));
    expect(usePlugins.getState().records[0]?.enabled).toBe(false);
  });

  it("retracts availability if the daemon disconnects during a list request", async () => {
    let finish!: (records: PluginRecord[]) => void;
    bridge.list.mockImplementation(() => new Promise<PluginRecord[]>((resolve) => { finish = resolve; }));
    stopSync = usePlugins.getState().startSync();
    useDaemon.setState({ link: "offline" });
    await act(async () => finish([record]));
    expect(usePlugins.getState().ready).toBe(false);
    expect(usePlugins.getState().error).toContain("offline");
  });
});

describe("plugin trust review", () => {
  it("associates the review label with the checkbox that gates trust", () => {
    usePlugins.setState({ records: [{ ...record, enabled: false, approvedDigest: "" }] });
    render(<PluginSettings />);
    fireEvent.click(screen.getByRole("button", { name: "Inspect capabilities & manifest" }));
    const review = screen.getByRole("checkbox", { name: /reviewed.*manifest/i });
    const trust = screen.getByRole("button", { name: "Trust and enable" });
    expect(trust).toBeDisabled();
    fireEvent.click(screen.getByText(/I reviewed the complete manifest/));
    expect(review).toBeChecked();
    expect(trust).toBeEnabled();
    fireEvent.click(screen.getByText(/I reviewed the complete manifest/));
    expect(review).not.toBeChecked();
    expect(trust).toBeDisabled();
  });
});

describe("manifest import inspection", () => {
  it("rejects non-object arguments before presenting an actionable preview", () => {
    expect(() => parseManifest(JSON.stringify({ ...record.manifest, actions: [{ ...record.manifest.actions[0], arguments: [] }] }))).toThrow("JSON object");
  });

  it("bounds the UTF-8 file size rather than counting characters", () => {
    expect(() => parseManifest(JSON.stringify({ ...record.manifest, description: "é".repeat(40_000) }))).toThrow("64 KiB");
  });
});
