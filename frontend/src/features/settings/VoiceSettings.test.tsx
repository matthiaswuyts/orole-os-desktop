import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ModelStatus, VoiceStatus } from "@/mygo";

const api = vi.hoisted(() => ({
  status: vi.fn(), download: vi.fn(), cancel: vi.fn(), remove: vi.fn(), use: vi.fn(), setLanguage: vi.fn(),
  changed: [] as ((status: unknown) => void)[],
}));
const env = vi.hoisted(() => ({ desktop: true }));
vi.mock("@/lib/backend", () => ({ get inDesktop() { return env.desktop; } }));
vi.mock("@/mygo", () => ({
  Dictation: { status: api.status, download: api.download, cancel: api.cancel, remove: api.remove, use: api.use, setLanguage: api.setLanguage },
  events: { dictationChanged: { on: (cb: (status: unknown) => void) => { api.changed.push(cb); return () => { api.changed = api.changed.filter((c) => c !== cb); }; } } },
}));
import { useDictation } from "@/stores/dictation";
import { VoiceSettings } from "./VoiceSettings";

const PARAKEET = "parakeet-tdt-0.6b-v3";
const WHISPER = "whisper-large-v3-turbo";
const parakeet = (over: Partial<ModelStatus> = {}): ModelStatus => ({
  id: PARAKEET, name: "Parakeet TDT 0.6B v3", description: "Fast. Finds the spoken language itself.", languageChoice: false,
  sizeBytes: 670478772, downloadBytes: 670478772, state: "absent", receivedBytes: 0, totalBytes: 0,
  license: "NVIDIA Parakeet TDT 0.6B v3, CC BY 4.0", ...over,
});
const whisper = (over: Partial<ModelStatus> = {}): ModelStatus => ({
  id: WHISPER, name: "Whisper large-v3 turbo", description: "Slower. About 100 languages.", languageChoice: true,
  sizeBytes: 1036613791, downloadBytes: 1036613791, state: "absent", receivedBytes: 0, totalBytes: 0,
  license: "OpenAI Whisper large-v3 turbo, MIT", ...over,
});
const status = (models: ModelStatus[], over: Partial<VoiceStatus> = {}): VoiceStatus => ({
  supported: true, ready: false, model: "", language: "", models,
  languages: [{ code: "nl", name: "Dutch" }, { code: "en", name: "English" }],
  maxSeconds: 120, sampleRate: 16000, partBytes: 1 << 20, ...over,
});
const emit = (next: VoiceStatus) => act(() => api.changed.forEach((cb) => cb(next)));
const row = (name: string) => screen.getByText(name, { selector: ".font-medium" }).closest<HTMLElement>(".px-3")!;
const language = () => screen.getByRole("combobox", { name: "Dictation language" });

beforeEach(() => {
  vi.clearAllMocks();
  env.desktop = true;
  api.changed = [];
  useDictation.setState({ status: null, loading: false, error: null, busy: {}, actionError: null });
  api.status.mockResolvedValue(status([parakeet(), whisper()]));
});
afterEach(cleanup);

describe("models", () => {
  it("shows each model's download size, the speech engine included while it is missing", async () => {
    api.status.mockResolvedValue(status([parakeet({ downloadBytes: 678014852 }), whisper({ downloadBytes: 1044149871 })]));
    render(<VoiceSettings />);
    expect(await within(await waitFor(() => row("Parakeet TDT 0.6B v3"))).findByText("Download size: 646.6 MB")).toBeInTheDocument();
    expect(within(row("Whisper large-v3 turbo")).getByText("Download size: 995.8 MB")).toBeInTheDocument();
    expect(within(row("Parakeet TDT 0.6B v3")).getByText("Fast. Finds the spoken language itself.")).toBeInTheDocument();
    expect(screen.getByText(/CC BY 4\.0; OpenAI Whisper large-v3 turbo, MIT\. Runs with sherpa-onnx \(Apache 2\.0\)/)).toBeInTheDocument();
    expect(screen.getByText(/Recordings and their text never leave it and are not saved/)).toBeInTheDocument();
  });

  it("downloads, shows live progress, and cancels", async () => {
    const downloading = status([parakeet({ state: "downloading", totalBytes: 670478772 }), whisper()]);
    api.download.mockResolvedValue(downloading);
    api.cancel.mockResolvedValue(status([parakeet(), whisper()]));
    render(<VoiceSettings />);
    const parakeetRow = await waitFor(() => row("Parakeet TDT 0.6B v3"));
    fireEvent.click(within(parakeetRow).getByRole("button", { name: "Download" }));
    expect(api.download).toHaveBeenCalledExactlyOnceWith({ id: PARAKEET });
    expect(await within(row("Parakeet TDT 0.6B v3")).findByText("0.0 of 639.4 MB")).toBeInTheDocument();

    emit(status([parakeet({ state: "downloading", receivedBytes: 327584768, totalBytes: 670478772 }), whisper()]));
    const progress = within(row("Parakeet TDT 0.6B v3")).getByRole("progressbar", { name: "Parakeet TDT 0.6B v3" });
    expect(progress).toHaveAttribute("aria-valuenow", String((327584768 / 670478772) * 100));
    expect(within(row("Parakeet TDT 0.6B v3")).getByText("312.4 of 639.4 MB")).toBeInTheDocument();

    fireEvent.click(within(row("Parakeet TDT 0.6B v3")).getByRole("button", { name: "Cancel" }));
    expect(api.cancel).toHaveBeenCalledExactlyOnceWith({ id: PARAKEET });
    expect(await within(row("Parakeet TDT 0.6B v3")).findByRole("button", { name: "Download" })).toBeInTheDocument();
  });

  it("marks the model in use and offers Use for another downloaded one", async () => {
    api.status.mockResolvedValue(status([parakeet({ state: "ready" }), whisper({ state: "ready" })], { ready: true, model: PARAKEET }));
    api.use.mockResolvedValue(status([parakeet({ state: "ready" }), whisper({ state: "ready" })], { ready: true, model: WHISPER }));
    render(<VoiceSettings />);
    expect(await within(await waitFor(() => row("Parakeet TDT 0.6B v3"))).findByText("In use")).toBeInTheDocument();
    expect(within(row("Parakeet TDT 0.6B v3")).queryByRole("button", { name: "Use" })).not.toBeInTheDocument();
    fireEvent.click(within(row("Whisper large-v3 turbo")).getByRole("button", { name: "Use" }));
    expect(api.use).toHaveBeenCalledExactlyOnceWith({ id: WHISPER });
    expect(await within(row("Whisper large-v3 turbo")).findByText("In use")).toBeInTheDocument();
    expect(within(row("Parakeet TDT 0.6B v3")).getByRole("button", { name: "Use" })).toBeInTheDocument();
  });

  it("shows why a download failed and retries it", async () => {
    api.status.mockResolvedValue(status([parakeet({ state: "failed", error: "The download was interrupted. Check your connection." }), whisper()]));
    api.download.mockRejectedValue(new Error("dictation_busy: Another download is running."));
    render(<VoiceSettings />);
    const failed = await waitFor(() => row("Parakeet TDT 0.6B v3"));
    expect(await within(failed).findByRole("alert")).toHaveTextContent("The download was interrupted. Check your connection.");
    fireEvent.click(within(failed).getByRole("button", { name: "Retry" }));
    expect(api.download).toHaveBeenCalledExactlyOnceWith({ id: PARAKEET });
    expect(await within(row("Parakeet TDT 0.6B v3")).findByText("Another download is running.")).toBeInTheDocument();
  });

  it("asks before removing a model and says how much space it frees", async () => {
    api.status.mockResolvedValue(status([parakeet({ state: "ready" }), whisper()], { ready: true, model: PARAKEET }));
    api.remove.mockResolvedValue(status([parakeet(), whisper()]));
    render(<VoiceSettings />);
    fireEvent.click(await screen.findByRole("button", { name: "Remove Parakeet TDT 0.6B v3" }));
    const dialog = await screen.findByRole("alertdialog");
    expect(within(dialog).getByText("Remove Parakeet TDT 0.6B v3?")).toBeInTheDocument();
    expect(within(dialog).getByText(/frees 639\.4 MB\. You can download it again/)).toBeInTheDocument();
    expect(api.remove).not.toHaveBeenCalled();
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument());
    expect(api.remove).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Remove Parakeet TDT 0.6B v3" }));
    fireEvent.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Remove" }));
    await waitFor(() => expect(api.remove).toHaveBeenCalledExactlyOnceWith({ id: PARAKEET }));
    expect(await within(row("Parakeet TDT 0.6B v3")).findByRole("button", { name: "Download" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Remove Parakeet TDT 0.6B v3" })).not.toBeInTheDocument();
  });

  it("offers no removal for a model that is not on this computer", async () => {
    render(<VoiceSettings />);
    await waitFor(() => row("Parakeet TDT 0.6B v3"));
    expect(screen.queryByRole("button", { name: /^Remove / })).not.toBeInTheDocument();
  });
});

describe("language", () => {
  it("is fixed to automatic for Parakeet and explains how to set one", async () => {
    api.status.mockResolvedValue(status([parakeet({ state: "ready" }), whisper()], { ready: true, model: PARAKEET }));
    render(<VoiceSettings />);
    await waitFor(() => expect(language()).toBeDisabled());
    expect(language()).toHaveTextContent("Automatic");
    expect(screen.getByText("Parakeet TDT 0.6B v3 finds the spoken language itself. To set a language, use Whisper large-v3 turbo.")).toBeInTheDocument();
  });

  it("can be set with Whisper", async () => {
    const ready = [parakeet(), whisper({ state: "ready" })];
    api.status.mockResolvedValue(status(ready, { ready: true, model: WHISPER }));
    api.setLanguage.mockResolvedValue(status(ready, { ready: true, model: WHISPER, language: "nl" }));
    render(<VoiceSettings />);
    await waitFor(() => expect(language()).toBeEnabled());
    expect(screen.getByText("Automatic detects it from what you say. Setting it helps with short recordings.")).toBeInTheDocument();
    fireEvent.pointerDown(language(), { button: 0, pointerType: "mouse" });
    fireEvent.mouseDown(language(), { button: 0 });
    const option = await screen.findByRole("option", { name: "Dutch" });
    const { promise: settled, resolve } = Promise.withResolvers<void>();
    setTimeout(resolve, 400);
    await settled;
    fireEvent.pointerUp(option, { button: 0, pointerType: "mouse" });
    fireEvent.mouseUp(option, { button: 0 });
    fireEvent.click(option);
    expect(api.setLanguage).toHaveBeenCalledExactlyOnceWith({ language: "nl" });
    await waitFor(() => expect(language()).toHaveTextContent("Dutch"));
  });
});

describe("states", () => {
  it("says voice settings need the desktop app", () => {
    env.desktop = false;
    render(<VoiceSettings />);
    expect(screen.getByText("Voice settings are available in the Orole OS desktop app.")).toBeInTheDocument();
    expect(api.status).not.toHaveBeenCalled();
  });

  it("explains a computer the speech engine cannot run on, with nothing to download", async () => {
    api.status.mockResolvedValue(status([parakeet(), whisper()], { supported: false }));
    render(<VoiceSettings />);
    expect(await screen.findByText(/Dictation needs Windows on an x64 processor/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Download" })).not.toBeInTheDocument();
  });

  it("shows a status failure with a retry", async () => {
    api.status.mockRejectedValueOnce(new Error("internal: daemon unavailable"));
    render(<VoiceSettings />);
    expect(await screen.findByText("daemon unavailable")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("Download size: 639.4 MB")).toBeInTheDocument();
    expect(api.status).toHaveBeenCalledTimes(2);
  });
});
