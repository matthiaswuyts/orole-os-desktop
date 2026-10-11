import { useEffect, useRef, useState } from "react";
import { MicIcon, SquareIcon, XIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Popover, PopoverPopup, PopoverTitle, PopoverTrigger } from "@/components/ui/popover";
import { errText } from "@/features/agents/format";
import { inDesktop } from "@/lib/backend";
import { Dictation } from "@/mygo";
import { useUI } from "@/stores/ui";

type VoiceState = "idle" | "starting" | "recording" | "transcribing";
type Readiness = { kind: "checking" } | { kind: "ready"; minutes: number } | { kind: "no-key" } | { kind: "unavailable"; message: string };

/** Mirrors the daemon's caps until `Dictation.status` reports them; the daemon enforces them either way. */
const DEFAULT_LIMITS = { maxSeconds: 120, maxBytes: 2 << 20 };
/** Speech-grade Opus: about 4 KB per second, so two minutes stay far below the size cap. */
const AUDIO_BITS_PER_SECOND = 32_000;
const SLICE_MS = 1000;
const MIME_CANDIDATES = ["audio/webm;codecs=opus", "audio/webm", "audio/ogg;codecs=opus", "audio/mp4"];

interface Run {
  stream?: MediaStream;
  recorder?: MediaRecorder;
  chunks: Blob[];
  bytes: number;
  startedAt: number;
  timer?: number;
  /** Set when the user cancels or the component goes away: late recorder events must do nothing. */
  discarded: boolean;
  limitNotice: string;
}

const pickMime = () => typeof MediaRecorder.isTypeSupported === "function" ? MIME_CANDIDATES.find((type) => MediaRecorder.isTypeSupported(type)) : undefined;

/** Stops the timer, the recorder and every microphone track. Safe to call twice. */
function release(run: Run) {
  window.clearInterval(run.timer);
  run.timer = undefined;
  const recorder = run.recorder;
  if (recorder) {
    recorder.ondataavailable = recorder.onerror = recorder.onstop = null;
    if (recorder.state !== "inactive") {
      try { recorder.stop(); } catch { /* already stopped */ }
    }
  }
  run.stream?.getTracks().forEach((track) => track.stop());
}

function micError(failure: unknown): string {
  const name = typeof failure === "object" && failure !== null && "name" in failure ? String(failure.name) : "";
  switch (name) {
    case "NotAllowedError":
    case "SecurityError":
      return "Microphone access was denied. Allow microphone access for Orole OS in Windows Settings > Privacy & security > Microphone, then try again, or type your message.";
    case "NotFoundError":
    case "OverconstrainedError":
    case "DevicesNotFoundError":
      return "No microphone was found. Connect a microphone and try again, or type your message.";
    case "NotReadableError":
    case "AbortError":
    case "TrackStartError":
      return "The microphone could not be opened. Another app may be using it. Close that app and try again.";
    default:
      return `Could not start recording: ${errText(failure)}. You can still type your message.`;
  }
}

/** The daemon's dictation errors arrive as "dictation_<code>: <sentence>" and the sentence is user-ready. */
function daemonError(failure: unknown): { code: string; message: string } {
  const text = errText(failure);
  const match = /^dictation_([a-z_]+): ([\s\S]*)$/.exec(text);
  if (match) return { code: match[1]!, message: match[2]! };
  return { code: "", message: `Dictation failed: ${text.replace(/^(bad_request|not_found|conflict|canceled|timeout|internal|denied): /, "")}` };
}

/** Base64 of the blob's bytes (the data URL's payload). */
function blobBase64(blob: Blob): Promise<string> {
  const { promise, resolve, reject } = Promise.withResolvers<string>();
  const reader = new FileReader();
  reader.onerror = () => reject(reader.error ?? new Error("could not read the recording"));
  reader.onload = () => {
    const url = String(reader.result);
    resolve(url.slice(url.indexOf(",") + 1));
  };
  reader.readAsDataURL(blob);
  return promise;
}

/**
 * Records the microphone with MediaRecorder, sends the finished recording to the daemon, and delivers the
 * transcript to the caller's editable draft. Starting is always a separate, disclosed consent action; audio goes
 * to OpenAI with the user's own key, held by the daemon. Nothing is recorded before consent and nothing is sent
 * to the agent automatically.
 */
export function ComposerVoice({ disabled, compact = false, quiet = false, onTranscript, onActiveChange }: {
  disabled: boolean;
  /** A round icon button and an inline one-line recording state, for a single-line composer. */
  compact?: boolean;
  /** With `compact`: small ghost round buttons instead of the primary-filled large ones, for a toolbar that has its own Send button. */
  quiet?: boolean;
  onTranscript: (text: string) => void;
  onActiveChange: (active: boolean) => void;
}) {
  const [open, setOpen] = useState(false);
  const [state, setState] = useState<VoiceState>("idle");
  const [seconds, setSeconds] = useState(0);
  const [readiness, setReadiness] = useState<Readiness>({ kind: "checking" });
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [needsKey, setNeedsKey] = useState(false);
  const active = useRef<Run | null>(null);
  const limits = useRef(DEFAULT_LIMITS);
  const callbacks = useRef({ onTranscript, onActiveChange });
  callbacks.current = { onTranscript, onActiveChange };

  // Late recorder or network results must never edit the next task's draft, and the microphone must never stay open.
  useEffect(() => () => {
    const run = active.current;
    active.current = null;
    if (run) {
      run.discarded = true;
      release(run);
    }
  }, []);

  const toggle = (next: boolean) => {
    setOpen(next);
    if (next) setReadiness({ kind: "checking" });
  };
  useEffect(() => {
    if (!open || !inDesktop) return;
    let live = true;
    Dictation.status().then((status) => {
      if (!live) return;
      limits.current = { maxSeconds: status.maxSeconds || DEFAULT_LIMITS.maxSeconds, maxBytes: status.maxBytes || DEFAULT_LIMITS.maxBytes };
      setReadiness(status.ready ? { kind: "ready", minutes: Math.round(limits.current.maxSeconds / 60) } : { kind: "no-key" });
    }).catch((failure: unknown) => {
      if (live) setReadiness({ kind: "unavailable", message: `Could not check dictation: ${daemonError(failure).message}` });
    });
    return () => { live = false; };
  }, [open]);
  const shown: Readiness = inDesktop ? readiness : { kind: "unavailable", message: "Dictation is available in the Orole OS desktop app." };

  const end = (run: Run) => {
    if (active.current !== run) return false;
    active.current = null;
    run.discarded = true;
    release(run);
    setState("idle");
    callbacks.current.onActiveChange(false);
    return true;
  };

  const fail = (run: Run, message: string, key = false) => {
    if (!end(run)) return;
    setError(message);
    setNeedsKey(key);
  };

  const transcribe = async (run: Run, blob: Blob, durationMs: number) => {
    try {
      if (blob.size === 0) return fail(run, "No audio was captured. Check that your microphone is working and try again.");
      if (blob.size > limits.current.maxBytes) return fail(run, "That recording is too large to transcribe. Record a shorter message.");
      const dataBase64 = await blobBase64(blob);
      if (active.current !== run) return;
      const result = await Dictation.transcribe({ mimeType: blob.type, dataBase64, durationMs });
      if (!end(run)) return;
      const text = result.text.trim();
      setNotice(text
        ? `Dictation added to your draft. Review and edit it before sending.${run.limitNotice}`
        : `No speech was recognized. Your draft is unchanged.${run.limitNotice}`);
      if (text) callbacks.current.onTranscript(text);
    } catch (failure) {
      const { code, message } = daemonError(failure);
      fail(run, message, code === "not_connected");
    }
  };

  const start = async () => {
    if (disabled || active.current) return;
    setError("");
    setNotice("");
    setNeedsKey(false);
    setSeconds(0);
    setOpen(false);
    const run: Run = { chunks: [], bytes: 0, startedAt: 0, discarded: false, limitNotice: "" };
    active.current = run;
    setState("starting");
    callbacks.current.onActiveChange(true);
    try {
      if (!navigator.mediaDevices?.getUserMedia || typeof MediaRecorder === "undefined") {
        throw new Error("this window cannot record audio");
      }
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      run.stream = stream;
      if (active.current !== run) { release(run); return; }
      const type = pickMime();
      const recorder = new MediaRecorder(stream, { ...(type ? { mimeType: type } : {}), audioBitsPerSecond: AUDIO_BITS_PER_SECOND });
      run.recorder = recorder;
      recorder.ondataavailable = ({ data }) => {
        if (run.discarded || data.size === 0) return;
        if (run.bytes + data.size > limits.current.maxBytes) {
          run.limitNotice = " The recording reached its size limit, so the end was left out.";
          stop(run);
          return;
        }
        run.chunks.push(data);
        run.bytes += data.size;
      };
      recorder.onerror = () => fail(run, "Recording failed. Check your microphone and try again.");
      recorder.onstop = () => {
        if (run.discarded || active.current !== run) return;
        const durationMs = Date.now() - run.startedAt;
        const blob = new Blob(run.chunks, { type: recorder.mimeType || type || "audio/webm" });
        // The microphone is released before the upload starts.
        run.stream?.getTracks().forEach((track) => track.stop());
        void transcribe(run, blob, durationMs);
      };
      run.startedAt = Date.now();
      recorder.start(SLICE_MS);
      setState("recording");
      run.timer = window.setInterval(() => {
        const elapsed = Math.floor((Date.now() - run.startedAt) / 1000);
        setSeconds(elapsed);
        if (elapsed >= limits.current.maxSeconds && !run.limitNotice) {
          run.limitNotice = ` Recording stopped at the ${Math.round(limits.current.maxSeconds / 60)}-minute limit.`;
          stop(run);
        }
      }, 1000);
    } catch (failure) {
      fail(run, micError(failure));
    }
  };

  /** Stop recording and hand the audio over for transcription. */
  const stop = (run: Run | null = active.current) => {
    if (!run || active.current !== run || !run.recorder || run.recorder.state === "inactive") return;
    window.clearInterval(run.timer);
    run.timer = undefined;
    setState("transcribing");
    try { run.recorder.stop(); }
    catch (failure) { fail(run, `Could not finish recording: ${errText(failure)}`); }
  };

  const cancel = () => {
    const run = active.current;
    if (!run) return;
    const sending = state === "transcribing";
    end(run);
    setNotice(sending ? "Dictation cancelled. Nothing was added to your draft." : "Dictation cancelled. Your draft is unchanged.");
  };

  const openProviders = () => {
    setOpen(false);
    useUI.getState().openSettings("ai");
  };
  const providersButton = <Button type="button" size="sm" onClick={openProviders}>Open AI providers</Button>;

  const messages = <>
    {notice && <p role="status" className="text-xs text-muted-foreground">{notice}</p>}
    {error && <p role="alert" className="text-xs text-destructive-foreground">{error}</p>}
    {error && needsKey && <div>{providersButton}</div>}
  </>;
  const disclosure = <PopoverPopup side="top" align={compact ? "end" : "start"} className="w-80">
    <div className="flex flex-col gap-3">
      <PopoverTitle>Dictate a message</PopoverTitle>
      {shown.kind === "checking" && <p role="status" className="text-sm text-muted-foreground">Checking dictation…</p>}
      {shown.kind === "unavailable" && <p role="status" className="text-sm">{shown.message}</p>}
      {shown.kind === "no-key" && <>
        <p role="status" className="text-sm">Dictation needs an OpenAI API key. Connect OpenAI in Settings &gt; AI providers.</p>
        {providersButton}
      </>}
      {shown.kind === "ready" && <>
        <p className="text-sm">Start records your microphone. When you stop, the recording is sent to OpenAI to be transcribed, using your OpenAI API key. It does not stay on this device.</p>
        <p className="text-xs text-muted-foreground">Orole does not save the recording or the transcript. Stop sends the recording and puts the text in an editable draft; Cancel discards it without sending. Recording stops after {shown.minutes} minutes. Nothing sends to the agent automatically.</p>
        <Button type="button" size="sm" onClick={() => void start()}>Start dictation</Button>
      </>}
    </div>
  </PopoverPopup>;
  const elapsed = `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
  const statusText = state === "starting" ? "Starting microphone…" : state === "recording" ? "Recording" : "Transcribing…";

  if (compact) {
    return <div className="relative flex shrink-0 items-center gap-2">
      {(notice || error) && <div className="absolute end-0 bottom-full mb-2 flex w-72 flex-col gap-1 rounded-lg border bg-popover p-2 shadow-lg/5">{messages}</div>}
      {state === "idle" ? <Popover open={open} onOpenChange={toggle}>
        <PopoverTrigger render={<Button type="button" size={quiet ? "icon-sm" : "icon-lg"} variant={quiet ? "ghost" : "default"} className="rounded-full" disabled={disabled} aria-label="Dictate a message" />}><MicIcon aria-hidden /></PopoverTrigger>
        {disclosure}
      </Popover> : <>
        <MicIcon aria-hidden className="size-4 text-warning" />
        <span role="status" className="text-xs">{statusText}</span>
        <span aria-label="Dictation elapsed time" className="font-mono text-xs tabular-nums">{elapsed}</span>
        <Button type="button" size={quiet ? "icon-sm" : "icon-lg"} variant={quiet ? "outline" : "default"} className="rounded-full" aria-label="Stop dictation" onClick={() => stop()} disabled={state !== "recording"}><SquareIcon aria-hidden className="fill-current" /></Button>
        <Button type="button" size={quiet ? "icon-sm" : "icon-lg"} variant="ghost" className="rounded-full" aria-label="Cancel dictation" onClick={cancel}><XIcon aria-hidden /></Button>
      </>}
    </div>;
  }

  return <div className="flex min-w-0 flex-col gap-2">
    {state === "idle" ? <Popover open={open} onOpenChange={toggle}>
      <PopoverTrigger render={<Button type="button" size="sm" variant="ghost" disabled={disabled} aria-label="Dictate a message" />}><MicIcon aria-hidden data-icon="inline-start" />Dictate</PopoverTrigger>
      {disclosure}
    </Popover> : <div className="flex flex-wrap items-center gap-2">
      <MicIcon aria-hidden className="size-4 text-warning" />
      <span role="status" className="text-xs">{statusText}</span>
      <span aria-label="Dictation elapsed time" className="font-mono text-xs tabular-nums">{elapsed}</span>
      <Button type="button" size="xs" variant="outline" onClick={() => stop()} disabled={state !== "recording"}><SquareIcon aria-hidden data-icon="inline-start" />Stop</Button>
      <Button type="button" size="xs" variant="ghost" onClick={cancel}>Cancel dictation</Button>
    </div>}
    {messages}
  </div>;
}
