import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ManifestRecord, ProviderManifest, ProviderStatus } from "@/mygo";

const bridge = vi.hoisted(() => ({
  providers: vi.fn(), defaults: vi.fn(), models: vi.fn(), connect: vi.fn(), disconnect: vi.fn(), test: vi.fn(), setDefaults: vi.fn(),
  manifests: vi.fn(), saveManifest: vi.fn(), trustManifest: vi.fn(), removeManifest: vi.fn(),
}));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({
  LLM: bridge,
  Shell: { openExternal: vi.fn() },
  Daemon: { health: vi.fn() },
  events: { llmChanged: { on: () => () => {} }, daemonState: { on: () => () => {} } },
}));
import { AIProviders } from "@/features/settings/AIProviders";
import { useLLM } from "@/stores/llm";
import { useDaemon } from "@/stores/daemon";

const DIGEST = "a".repeat(64);
const NEXT = "b".repeat(64);
const manifest: ProviderManifest = {
  schemaVersion: 1, id: "ollama", name: "Ollama", baseUrl: "http://localhost:11434/v1", auth: { type: "none" },
  headers: { "X-Team": "orole" }, models: [{ id: "llama3.2", vision: false, pdf: false, tools: true, reasoning: false }],
};
const pending: ManifestRecord = { manifest, digest: DIGEST, enabled: false, approvedDigest: "", updatedAt: 1 };
const trusted: ManifestRecord = { ...pending, enabled: true, approvedDigest: DIGEST };
const hosted: ProviderManifest = { schemaVersion: 1, id: "gateway", name: "Gateway", baseUrl: "https://llm.example.com/v1", auth: { type: "bearer" } };
const hostedRecord: ManifestRecord = { manifest: hosted, digest: NEXT, enabled: true, approvedDigest: NEXT, updatedAt: 1 };

function seed(manifests: ManifestRecord[], providers: ProviderStatus[] = []) {
  bridge.providers.mockResolvedValue(providers);
  bridge.manifests.mockResolvedValue(manifests);
  useLLM.setState({ providers, manifests, ready: true, manifestsReady: true, defaults: { chatModel: "", memoryModel: "" } });
}

beforeEach(() => {
  for (const fn of Object.values(bridge)) fn.mockReset();
  bridge.providers.mockResolvedValue([]);
  bridge.manifests.mockResolvedValue([]);
  bridge.defaults.mockResolvedValue({ chatModel: "", memoryModel: "" });
  bridge.models.mockResolvedValue([]);
  useDaemon.setState({ link: "connected" });
  useLLM.setState({ providers: [], defaults: null, models: {}, busy: {}, manifests: [], manifestsReady: false, manifestsError: null, manifestBusy: {}, ready: false, loading: false, error: null });
});
afterEach(cleanup);

const manifestRow = (name: string) => screen.getByRole("listitem", { name: `${name} manifest` });
const importJson = JSON.stringify(manifest);

async function openImport(source: string) {
  fireEvent.click(await screen.findByRole("button", { name: "Add provider" }));
  fireEvent.change(await screen.findByLabelText("Manifest JSON"), { target: { value: source } });
  fireEvent.click(screen.getByRole("button", { name: "Review" }));
}

describe("provider manifests UI", () => {
  it("states what manifests do and do not contain", async () => {
    seed([]);
    render(<AIProviders />);
    expect(await screen.findByText(/contain no secrets/)).toBeInTheDocument();
    expect(screen.getByText(/localhost, 127\.0\.0\.1 and ::1/)).toBeInTheDocument();
    expect(screen.getByText("No custom providers yet.")).toBeInTheDocument();
  });

  it("reviews a pasted manifest locally, then imports it disabled with an empty expected digest", async () => {
    seed([]);
    bridge.saveManifest.mockResolvedValue(pending);
    render(<AIProviders />);
    fireEvent.click(await screen.findByRole("button", { name: "Add provider" }));
    expect(screen.getByLabelText("Example manifest")).toHaveTextContent('"baseUrl": "http://localhost:11434/v1"');
    fireEvent.change(screen.getByLabelText("Manifest JSON"), { target: { value: importJson } });
    fireEvent.click(screen.getByRole("button", { name: "Review" }));
    expect(bridge.saveManifest).not.toHaveBeenCalled();
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("localhost:11434")).toBeInTheDocument();
    expect(within(dialog).getByText(/No API key is stored or sent/)).toBeInTheDocument();
    expect(within(dialog).getByText("X-Team: orole")).toBeInTheDocument();
    expect(within(dialog).getByText("llama3.2")).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Import disabled provider" }));
    await waitFor(() => expect(bridge.saveManifest).toHaveBeenCalledWith({ manifest, expectedDigest: "" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(within(manifestRow("Ollama")).getByText("Disabled / needs review")).toBeInTheDocument();
  });

  it("rejects malformed JSON locally without calling the daemon", async () => {
    seed([]);
    render(<AIProviders />);
    await openImport("{ nope");
    expect(await screen.findByText(/not valid JSON/)).toBeInTheDocument();
    expect(bridge.saveManifest).not.toHaveBeenCalled();
  });

  it("shows the daemon's validation error verbatim and keeps the review open", async () => {
    seed([]);
    bridge.saveManifest.mockRejectedValue(new Error("bad_request: baseUrl must be an https URL (http is only allowed for localhost, 127.0.0.1 and ::1)"));
    render(<AIProviders />);
    await openImport(importJson);
    fireEvent.click(await screen.findByRole("button", { name: "Import disabled provider" }));
    expect(await screen.findByText("baseUrl must be an https URL (http is only allowed for localhost, 127.0.0.1 and ::1)")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Import disabled provider" })).toBeInTheDocument();
  });

  it("requires the review checkbox, shows the waiting state, then enables after approval", async () => {
    seed([pending]);
    let approve!: (record: ManifestRecord) => void;
    bridge.trustManifest.mockReturnValue(new Promise<ManifestRecord>((resolve) => { approve = resolve; }));
    render(<AIProviders />);
    fireEvent.click(within(manifestRow("Ollama")).getByRole("button", { name: "Review & trust" }));
    const dialog = await screen.findByRole("dialog");
    const confirm = within(dialog).getByRole("button", { name: "Trust and enable" });
    expect(confirm).toBeDisabled();
    expect(within(dialog).getByText(/separate approval request from the daemon/)).toBeInTheDocument();
    fireEvent.click(within(dialog).getByLabelText("I reviewed the host, auth and headers"));
    fireEvent.click(confirm);
    expect(await within(dialog).findByText(/Waiting for your approval/)).toBeInTheDocument();
    expect(bridge.trustManifest).toHaveBeenCalledWith({ id: "ollama", digest: DIGEST, enabled: true });
    approve(trusted);
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(within(manifestRow("Ollama")).getByText("Trusted and enabled")).toBeInTheDocument();
  });

  it("explains a denied approval and leaves the provider disabled", async () => {
    seed([pending]);
    bridge.trustManifest.mockRejectedValue(new Error("denied: policy: denied"));
    render(<AIProviders />);
    fireEvent.click(within(manifestRow("Ollama")).getByRole("button", { name: "Review & trust" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByLabelText("I reviewed the host, auth and headers"));
    fireEvent.click(within(dialog).getByRole("button", { name: "Trust and enable" }));
    expect(await within(dialog).findByText(/approval request was denied/)).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Trust and enable" })).toBeEnabled();
    expect(useLLM.getState().manifests[0].enabled).toBe(false);
  });

  it("explains a canceled approval", async () => {
    seed([pending]);
    bridge.trustManifest.mockRejectedValue(new Error("canceled: context canceled"));
    render(<AIProviders />);
    fireEvent.click(within(manifestRow("Ollama")).getByRole("button", { name: "Review & trust" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByLabelText("I reviewed the host, auth and headers"));
    fireEvent.click(within(dialog).getByRole("button", { name: "Trust and enable" }));
    expect(await within(dialog).findByText(/approval request was canceled/)).toBeInTheDocument();
  });

  it("blocks trusting a review whose manifest changed underneath it", async () => {
    seed([pending]);
    render(<AIProviders />);
    fireEvent.click(within(manifestRow("Ollama")).getByRole("button", { name: "Review & trust" }));
    const dialog = await screen.findByRole("dialog");
    useLLM.setState({ manifests: [{ ...pending, digest: NEXT }] });
    expect(await within(dialog).findByText("This manifest changed")).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Trust and enable" })).toBeDisabled();
  });

  it("revokes a trusted manifest", async () => {
    seed([trusted]);
    bridge.trustManifest.mockResolvedValue(pending);
    render(<AIProviders />);
    fireEvent.click(within(manifestRow("Ollama")).getByRole("button", { name: "Revoke" }));
    await waitFor(() => expect(bridge.trustManifest).toHaveBeenCalledWith({ id: "ollama", digest: DIGEST, enabled: false }));
    expect(await within(manifestRow("Ollama")).findByText("Disabled / needs review")).toBeInTheDocument();
  });

  it("saves an edit with the current digest and warns about re-review", async () => {
    seed([trusted]);
    bridge.saveManifest.mockResolvedValue({ ...pending, digest: NEXT, manifest: { ...manifest, name: "Ollama 2" } });
    render(<AIProviders />);
    fireEvent.click(within(manifestRow("Ollama")).getByRole("button", { name: "Edit JSON" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/requires a new review/)).toBeInTheDocument();
    expect(within(dialog).getByText(/stored API key is deleted/)).toBeInTheDocument();
    fireEvent.change(within(dialog).getByLabelText("Manifest JSON"), { target: { value: JSON.stringify({ ...manifest, name: "Ollama 2" }) } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(bridge.saveManifest).toHaveBeenCalledWith({ manifest: { ...manifest, name: "Ollama 2" }, expectedDigest: DIGEST }));
    expect(await screen.findByRole("listitem", { name: "Ollama 2 manifest" })).toBeInTheDocument();
  });

  it("surfaces an edit conflict and keeps the editor open", async () => {
    seed([pending]);
    bridge.saveManifest.mockRejectedValue(new Error("conflict: llm manifest: manifest or trust changed; reload and review again"));
    render(<AIProviders />);
    fireEvent.click(within(manifestRow("Ollama")).getByRole("button", { name: "Edit JSON" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Save changes" }));
    expect(await within(dialog).findByText(/manifest or trust changed; reload and review again/)).toBeInTheDocument();
    expect(within(dialog).getByText(/open the current version/)).toBeInTheDocument();
  });

  it("confirms removal, mentioning the stored key, before calling the daemon", async () => {
    seed([pending]);
    bridge.removeManifest.mockResolvedValue(undefined);
    render(<AIProviders />);
    fireEvent.click(within(manifestRow("Ollama")).getByRole("button", { name: "Remove" }));
    const dialog = await screen.findByRole("alertdialog");
    expect(within(dialog).getByText(/deletes the manifest and the API key stored for it/)).toBeInTheDocument();
    expect(bridge.removeManifest).not.toHaveBeenCalled();
    fireEvent.click(within(dialog).getByRole("button", { name: "Remove provider" }));
    await waitFor(() => expect(bridge.removeManifest).toHaveBeenCalledWith({ id: "ollama", digest: DIGEST }));
    await waitFor(() => expect(screen.queryByRole("listitem", { name: "Ollama manifest" })).toBeNull());
  });

  it("shows no Connect or Disconnect for an auth none provider", async () => {
    seed([trusted], [{ id: "ollama", name: "Ollama", kind: "manifest", connected: true }]);
    render(<AIProviders />);
    const row = await screen.findByRole("listitem", { name: "Ollama" });
    expect(within(row).getByText("Connected")).toBeInTheDocument();
    expect(within(row).getByRole("button", { name: "Test" })).toBeInTheDocument();
    expect(within(row).queryByRole("button", { name: "Disconnect" })).toBeNull();
    expect(within(row).queryByRole("button", { name: /Connect|Update key/ })).toBeNull();
  });

  it("uses the normal Connect flow for a trusted manifest that needs a key", async () => {
    seed([hostedRecord], [{ id: "gateway", name: "Gateway", kind: "manifest", connected: false }]);
    render(<AIProviders />);
    const row = await screen.findByRole("listitem", { name: "Gateway" });
    const connect = within(row).getByRole("button", { name: "Connect" });
    expect(connect).toBeEnabled();
  });
});

describe("manifest store", () => {
  it("drops a list response that a newer save has overtaken", async () => {
    let late!: (records: ManifestRecord[]) => void;
    bridge.manifests.mockReturnValueOnce(new Promise<ManifestRecord[]>((resolve) => { late = resolve; }));
    bridge.saveManifest.mockResolvedValue(pending);
    const load = useLLM.getState().loadManifests();
    await useLLM.getState().saveManifest(manifest, "");
    late([]);
    await load;
    expect(useLLM.getState().manifests).toEqual([pending]);
  });

  it("keeps only the newest of two overlapping loads", async () => {
    const resolvers: Array<(records: ManifestRecord[]) => void> = [];
    bridge.manifests.mockImplementation(() => new Promise<ManifestRecord[]>((resolve) => { resolvers.push(resolve); }));
    const first = useLLM.getState().loadManifests();
    const second = useLLM.getState().loadManifests();
    resolvers[1]([trusted]);
    await second;
    resolvers[0]([]);
    await first;
    expect(useLLM.getState().manifests).toEqual([trusted]);
  });

  it("returns the daemon's error code and clears busy state", async () => {
    bridge.trustManifest.mockRejectedValue(new Error("denied: policy: denied"));
    const outcome = await useLLM.getState().trustManifest("ollama", DIGEST, true);
    expect(outcome).toEqual({ ok: false, error: "policy: denied", code: "denied" });
    expect(useLLM.getState().manifestBusy).toEqual({});
  });
});
