import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({ status: vi.fn(), transcribe: vi.fn(), openSettings: vi.fn(), getUserMedia: vi.fn() }));
const env = vi.hoisted(() => ({ desktop: true }));
vi.mock("@/lib/backend", () => ({ get inDesktop() { return env.desktop; } }));
vi.mock("@/mygo", () => ({ Dictation: { status: api.status, transcribe: api.transcribe } }));
vi.mock("@/stores/ui", () => ({ useUI: { getState: () => ({ openSettings: api.openSettings }) } }));
import { ComposerVoice } from "./ComposerVoice";

class RecorderDouble {
  static instances: RecorderDouble[] = [];
  static isTypeSupported = vi.fn((type: string) => type === "audio/webm;codecs=opus");
  state: "inactive" | "recording" = "inactive";
  mimeType: string;
  ondataavailable: ((event: { data: Blob }) => void) | null = null;
  onstop: (() => void) | null = null;
  onerror: (() => void) | null = null;
  start = vi.fn(() => { this.state = "recording"; });
  /** Like the real recorder: flush the last chunk, then report the stop. */
  stop = vi.fn(() => {
    this.state = "inactive";
    this.ondataavailable?.({ data: new Blob(["voice"], { type: this.mimeType }) });
    this.onstop?.();
  });
  readonly stream: unknown;
  readonly options: { mimeType?: string; audioBitsPerSecond?: number };
  constructor(stream: unknown, options: { mimeType?: string; audioBitsPerSecond?: number }) {
    this.stream = stream;
    this.options = options;
    this.mimeType = options.mimeType ?? "";
    RecorderDouble.instances.push(this);
  }
}

const track = { stop: vi.fn() };
const stream = { getTracks: () => [track] };
const status = (over: Partial<{ ready: boolean; maxSeconds: number; maxBytes: number }> = {}) => ({ ready: true, provider: "OpenAI", maxSeconds: 120, maxBytes: 2 << 20, ...over });
interface Reply { text: string; model: string }
const reply = (text: string): Reply => ({ text, model: "gpt-4o-mini-transcribe" });

/** A draft box beside the voice control, like the composer that embeds it. */
function Harness({ compact = false, onActive = () => {}, onSend = () => {} }: { compact?: boolean; onActive?: (active: boolean) => void; onSend?: () => void }) {
  const [text, setText] = useState("");
  const [active, setActive] = useState(false);
  return <div>
    <textarea aria-label="Message the agent" value={text} onChange={(event) => setText(event.target.value)} />
    <button type="button" disabled={active} onClick={onSend}>Send</button>
    <ComposerVoice compact={compact} disabled={false}
      onActiveChange={(value) => { setActive(value); onActive(value); }}
      onTranscript={(value) => setText((current) => current ? `${current}${/\s$/.test(current) ? "" : " "}${value}` : value)} />
  </div>;
}

const draft = () => screen.getByLabelText("Message the agent");
const openPopover = () => fireEvent.click(screen.getByRole("button", { name: "Dictate a message" }));
async function startRecording() {
  openPopover();
  fireEvent.click(await screen.findByRole("button", { name: "Start dictation" }));
  await waitFor(() => expect(screen.getByRole("button", { name: "Stop" })).toBeEnabled());
  return RecorderDouble.instances.at(-1)!;
}

beforeEach(() => {
  vi.clearAllMocks();
  env.desktop = true;
  RecorderDouble.instances = [];
  RecorderDouble.isTypeSupported.mockImplementation((type: string) => type === "audio/webm;codecs=opus");
  vi.stubGlobal("MediaRecorder", RecorderDouble);
  Object.defineProperty(navigator, "mediaDevices", { configurable: true, value: { getUserMedia: api.getUserMedia } });
  api.getUserMedia.mockResolvedValue(stream);
  api.status.mockResolvedValue(status());
  api.transcribe.mockResolvedValue(reply("Recognized sentence."));
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  Reflect.deleteProperty(navigator, "mediaDevices");
});

describe("consent", () => {
  it("discloses OpenAI processing and records nothing until Start is pressed", async () => {
    render(<Harness />);
    openPopover();
    expect(await screen.findByText(/the recording is sent to OpenAI to be transcribed, using your OpenAI API key/)).toBeInTheDocument();
    expect(api.getUserMedia).not.toHaveBeenCalled();
    expect(RecorderDouble.instances).toHaveLength(0);
    expect(api.transcribe).not.toHaveBeenCalled();
  });

  it("shows an honest setup state, with no recording, when no OpenAI key is connected", async () => {
    api.status.mockResolvedValue(status({ ready: false }));
    render(<Harness />);
    openPopover();
    expect(await screen.findByText("Dictation needs an OpenAI API key. Connect OpenAI in Settings > AI providers.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Start dictation" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open AI providers" }));
    expect(api.openSettings).toHaveBeenCalledExactlyOnceWith("ai");
    expect(api.getUserMedia).not.toHaveBeenCalled();
  });

  it("reports a status failure instead of offering to record", async () => {
    api.status.mockRejectedValue(new Error("daemon unavailable: pipe closed"));
    render(<Harness />);
    openPopover();
    expect(await screen.findByText(/Could not check dictation: Dictation failed: daemon unavailable: pipe closed/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Start dictation" })).not.toBeInTheDocument();
  });
});

describe("recording", () => {
  it("records, then Stop transcribes into an editable draft that is never sent", async () => {
    const active = vi.fn();
    const send = vi.fn();
    render(<Harness onActive={active} onSend={send} />);
    fireEvent.change(draft(), { target: { value: "Existing draft." } });
    const recorder = await startRecording();
    expect(api.getUserMedia).toHaveBeenCalledExactlyOnceWith({ audio: true });
    expect(recorder.options).toEqual({ mimeType: "audio/webm;codecs=opus", audioBitsPerSecond: 32_000 });
    expect(recorder.start).toHaveBeenCalledOnce();
    expect(active).toHaveBeenLastCalledWith(true);
    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled();

    const pending = Promise.withResolvers<Reply>();
    api.transcribe.mockReturnValue(pending.promise);
    fireEvent.click(screen.getByRole("button", { name: "Stop" }));
    expect(await screen.findByText("Transcribing…")).toBeInTheDocument();
    expect(recorder.stop).toHaveBeenCalledOnce();
    expect(track.stop).toHaveBeenCalled(); // the microphone is released before the upload
    await waitFor(() => expect(api.transcribe).toHaveBeenCalledExactlyOnceWith({ mimeType: "audio/webm;codecs=opus", dataBase64: btoa("voice"), durationMs: expect.any(Number) }));
    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled();
    expect(draft()).toHaveValue("Existing draft.");

    await act(async () => pending.resolve(reply("  Recognized sentence.  ")));
    expect(draft()).toHaveValue("Existing draft. Recognized sentence.");
    expect(active).toHaveBeenLastCalledWith(false);
    expect(screen.getByRole("button", { name: "Send" })).toBeEnabled();
    expect(screen.getByRole("status")).toHaveTextContent("Dictation added to your draft. Review and edit it before sending.");
    fireEvent.change(draft(), { target: { value: "Edited transcript" } });
    expect(send).not.toHaveBeenCalled();
  });

  it("falls back to a supported container and tells the daemon what it recorded", async () => {
    RecorderDouble.isTypeSupported.mockImplementation((type: string) => type === "audio/mp4");
    render(<Harness />);
    const recorder = await startRecording();
    expect(recorder.options.mimeType).toBe("audio/mp4");
    fireEvent.click(screen.getByRole("button", { name: "Stop" }));
    await waitFor(() => expect(api.transcribe).toHaveBeenCalledWith(expect.objectContaining({ mimeType: "audio/mp4" })));
  });

  it("lets the browser pick a container when none of the preferred ones is supported", async () => {
    RecorderDouble.isTypeSupported.mockReturnValue(false);
    render(<Harness />);
    const recorder = await startRecording();
    expect(recorder.options).toEqual({ audioBitsPerSecond: 32_000 });
    fireEvent.click(screen.getByRole("button", { name: "Stop" }));
    await waitFor(() => expect(api.transcribe).toHaveBeenCalledWith(expect.objectContaining({ mimeType: "audio/webm" })));
  });

  it("shows the elapsed time and stops on its own at the maximum duration", async () => {
    render(<Harness />);
    openPopover();
    const start = await screen.findByRole("button", { name: "Start dictation" });
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval", "Date"] });
    fireEvent.click(start);
    await act(async () => {});
    const recorder = RecorderDouble.instances[0]!;
    await act(async () => { vi.advanceTimersByTime(2100); });
    expect(screen.getByLabelText("Dictation elapsed time")).toHaveTextContent("0:02");
    expect(recorder.stop).not.toHaveBeenCalled();
    await act(async () => { vi.advanceTimersByTime(118_000); });
    expect(recorder.stop).toHaveBeenCalledOnce();
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 50)); }); // FileReader is real; only the interval and Date are faked
    expect(api.transcribe).toHaveBeenCalledOnce();
    expect(draft()).toHaveValue("Recognized sentence.");
    expect(screen.getByRole("status")).toHaveTextContent("Recording stopped at the 2-minute limit.");
  });

  it("stops when the recording reaches the size cap and drops the excess", async () => {
    api.status.mockResolvedValue(status({ maxBytes: 12 }));
    render(<Harness />);
    const recorder = await startRecording();
    act(() => recorder.ondataavailable?.({ data: new Blob(["0123456"]) }));
    act(() => recorder.ondataavailable?.({ data: new Blob(["too much"]) }));
    await waitFor(() => expect(api.transcribe).toHaveBeenCalledOnce());
    expect(recorder.stop).toHaveBeenCalledOnce();
    expect(api.transcribe).toHaveBeenCalledWith(expect.objectContaining({ dataBase64: btoa("0123456voice") }));
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("size limit"));
  });

  it("works in the compact layout", async () => {
    render(<Harness compact />);
    openPopover();
    fireEvent.click(await screen.findByRole("button", { name: "Start dictation" }));
    fireEvent.click(await screen.findByRole("button", { name: "Stop dictation" }));
    await waitFor(() => expect(draft()).toHaveValue("Recognized sentence."));
    expect(screen.getByRole("button", { name: "Dictate a message" })).toBeInTheDocument();
  });
});

describe("cancel and teardown", () => {
  it("cancel discards the recording without sending any audio", async () => {
    const active = vi.fn();
    render(<Harness onActive={active} />);
    fireEvent.change(draft(), { target: { value: "Keep this" } });
    const recorder = await startRecording();
    fireEvent.click(screen.getByRole("button", { name: "Cancel dictation" }));
    expect(track.stop).toHaveBeenCalled();
    expect(recorder.stop).toHaveBeenCalledOnce();
    expect(api.transcribe).not.toHaveBeenCalled();
    expect(draft()).toHaveValue("Keep this");
    expect(active).toHaveBeenLastCalledWith(false);
    expect(screen.getByRole("status")).toHaveTextContent("Dictation cancelled. Your draft is unchanged.");
    expect(screen.getByRole("button", { name: "Dictate a message" })).toBeInTheDocument();
  });

  it("cancel while transcribing ignores the late result", async () => {
    const pending = Promise.withResolvers<Reply>();
    api.transcribe.mockReturnValue(pending.promise);
    render(<Harness />);
    await startRecording();
    fireEvent.click(screen.getByRole("button", { name: "Stop" }));
    fireEvent.click(await screen.findByRole("button", { name: "Cancel dictation" }));
    await act(async () => pending.resolve(reply("Too late")));
    expect(draft()).toHaveValue("");
    expect(screen.getByRole("status")).toHaveTextContent("Nothing was added to your draft.");
  });

  it("releases the microphone when it unmounts while recording", async () => {
    const view = render(<Harness />);
    const recorder = await startRecording();
    track.stop.mockClear();
    view.unmount();
    expect(track.stop).toHaveBeenCalled();
    expect(recorder.stop).toHaveBeenCalledOnce();
    expect(api.transcribe).not.toHaveBeenCalled();
  });

  it("never delivers a transcript to a draft that has gone away (session switch)", async () => {
    const pending = Promise.withResolvers<Reply>();
    api.transcribe.mockReturnValue(pending.promise);
    const onTranscript = vi.fn();
    const view = render(<ComposerVoice disabled={false} onActiveChange={() => {}} onTranscript={onTranscript} />);
    openPopover();
    fireEvent.click(await screen.findByRole("button", { name: "Start dictation" }));
    fireEvent.click(await waitFor(() => { const stop = screen.getByRole("button", { name: "Stop" }); expect(stop).toBeEnabled(); return stop; }));
    await waitFor(() => expect(api.transcribe).toHaveBeenCalledOnce());
    view.unmount();
    await act(async () => pending.resolve(reply("Wrong task")));
    expect(onTranscript).not.toHaveBeenCalled();
  });

  it("releases the microphone if the component goes away while the permission prompt is open", async () => {
    const grant = Promise.withResolvers<typeof stream>();
    api.getUserMedia.mockReturnValue(grant.promise);
    const view = render(<Harness />);
    openPopover();
    fireEvent.click(await screen.findByRole("button", { name: "Start dictation" }));
    await waitFor(() => expect(api.getUserMedia).toHaveBeenCalledOnce());
    view.unmount();
    await act(async () => grant.resolve(stream));
    expect(track.stop).toHaveBeenCalled();
    expect(RecorderDouble.instances).toHaveLength(0);
  });
});

describe("errors", () => {
  const startAndStop = async () => {
    await startRecording();
    fireEvent.click(screen.getByRole("button", { name: "Stop" }));
    return screen.findByRole("alert");
  };
  const start = async () => {
    openPopover();
    fireEvent.click(await screen.findByRole("button", { name: "Start dictation" }));
    return screen.findByRole("alert");
  };

  it.each([
    ["denied", new DOMException("blocked", "NotAllowedError"), /Microphone access was denied.*Privacy & security > Microphone/],
    ["no microphone", new DOMException("none", "NotFoundError"), /No microphone was found/],
    ["busy microphone", new DOMException("busy", "NotReadableError"), /Another app may be using it/],
    ["unexpected", new Error("boom"), /Could not start recording: boom/],
  ])("explains a microphone failure: %s", async (_name, failure, message) => {
    api.getUserMedia.mockRejectedValue(failure);
    const active = vi.fn();
    render(<Harness onActive={active} />);
    expect(await start()).toHaveTextContent(message);
    expect(RecorderDouble.instances).toHaveLength(0);
    expect(active).toHaveBeenLastCalledWith(false);
    expect(screen.getByRole("button", { name: "Send" })).toBeEnabled();
  });

  it("says so when the window cannot record at all", async () => {
    Reflect.deleteProperty(navigator, "mediaDevices");
    render(<Harness />);
    expect(await start()).toHaveTextContent("this window cannot record audio");
  });

  it.each([
    ["auth", "dictation_auth: OpenAI rejected your API key. Reconnect OpenAI in Settings > AI providers.", "OpenAI rejected your API key."],
    ["quota", "dictation_quota: Your OpenAI account is out of credit or over its quota.", "out of credit or over its quota"],
    ["network", "dictation_network: Could not reach OpenAI to transcribe. Check your connection and try again.", "Could not reach OpenAI"],
    ["too long", "dictation_too_long: That recording is longer than 120 seconds. Record a shorter message.", "longer than 120 seconds"],
    ["too large", "dictation_too_large: That recording is larger than 2 MB. Record a shorter message.", "larger than 2 MB"],
    ["rate limit", "dictation_rate_limit: OpenAI is rate limiting requests. Wait a moment and try again.", "rate limiting"],
  ])("shows the daemon's sentence for %s without its machine prefix", async (_name, failure, message) => {
    api.transcribe.mockRejectedValue(new Error(failure));
    render(<Harness />);
    fireEvent.change(draft(), { target: { value: "Keep this" } });
    const alert = await startAndStop();
    expect(alert).toHaveTextContent(message);
    expect(alert).not.toHaveTextContent("dictation_");
    expect(draft()).toHaveValue("Keep this");
    expect(track.stop).toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "Open AI providers" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Send" })).toBeEnabled();
  });

  it("offers the AI providers settings when the key disappeared after the disclosure", async () => {
    api.transcribe.mockRejectedValue(new Error("dictation_not_connected: Dictation needs an OpenAI API key. Connect OpenAI in Settings > AI providers."));
    render(<Harness />);
    expect(await startAndStop()).toHaveTextContent("Dictation needs an OpenAI API key.");
    fireEvent.click(screen.getByRole("button", { name: "Open AI providers" }));
    expect(api.openSettings).toHaveBeenCalledExactlyOnceWith("ai");
  });

  it("describes any other failure plainly", async () => {
    api.transcribe.mockRejectedValue(new Error("internal: daemon unavailable"));
    render(<Harness />);
    expect(await startAndStop()).toHaveTextContent("Dictation failed: daemon unavailable");
  });

  it("reports an empty transcript and leaves the draft untouched", async () => {
    api.transcribe.mockResolvedValue(reply("   "));
    render(<Harness />);
    fireEvent.change(draft(), { target: { value: "Keep this" } });
    await startRecording();
    fireEvent.click(screen.getByRole("button", { name: "Stop" }));
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("No speech was recognized. Your draft is unchanged."));
    expect(draft()).toHaveValue("Keep this");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("reports a recording that captured no audio without calling the daemon", async () => {
    render(<Harness />);
    const recorder = await startRecording();
    recorder.stop.mockImplementation(() => { recorder.state = "inactive"; recorder.onstop?.(); });
    fireEvent.click(screen.getByRole("button", { name: "Stop" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("No audio was captured");
    expect(api.transcribe).not.toHaveBeenCalled();
  });

  it("reports a recorder failure and releases the microphone", async () => {
    render(<Harness />);
    const recorder = await startRecording();
    act(() => recorder.onerror?.());
    expect(await screen.findByRole("alert")).toHaveTextContent("Recording failed");
    expect(track.stop).toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Send" })).toBeEnabled();
  });
});

describe("outside the desktop app", () => {
  it("says dictation needs the desktop app instead of calling the daemon", async () => {
    env.desktop = false;
    render(<Harness />);
    openPopover();
    expect(await screen.findByText("Dictation is available in the Orole OS desktop app.")).toBeInTheDocument();
    expect(api.status).not.toHaveBeenCalled();
  });
});
