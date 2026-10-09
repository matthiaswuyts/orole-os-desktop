import { useEffect, useRef, useState } from "react";
import { MicIcon, SquareIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Popover, PopoverPopup, PopoverTitle, PopoverTrigger } from "@/components/ui/popover";
import { errText } from "@/features/agents/format";

interface SpeechResult {
  isFinal: boolean;
  readonly [index: number]: { transcript: string };
}
interface Recognition {
  lang: string;
  continuous: boolean;
  interimResults: boolean;
  onstart: (() => void) | null;
  onend: (() => void) | null;
  onerror: ((event: { error: string }) => void) | null;
  onresult: ((event: { results: ArrayLike<SpeechResult> }) => void) | null;
  start(): void;
  stop(): void;
  abort(): void;
}
type RecognitionConstructor = new () => Recognition;
type VoiceWindow = Omit<Window, "SpeechRecognition" | "webkitSpeechRecognition"> & { SpeechRecognition?: RecognitionConstructor; webkitSpeechRecognition?: RecognitionConstructor };
type VoiceState = "idle" | "starting" | "recording" | "finishing";

const speechErrors: Record<string, string> = {
  "not-allowed": "Microphone access was denied. Allow microphone access in your platform settings, or type your message.",
  "service-not-allowed": "This platform does not allow speech recognition. Type your message instead.",
  "audio-capture": "No microphone is available. Connect a microphone or type your message.",
  "network": "Speech recognition could not reach its service. Check your connection or type your message.",
  "no-speech": "No speech was recognized. Try again or type your message.",
  "language-not-supported": "Speech recognition does not support your current language. Type your message instead.",
  "aborted": "Dictation was interrupted. You can try again or type your message.",
};

/** Real Web Speech only. Starting is always a separate, disclosed consent action. */
export function ComposerVoice({ disabled, onTranscript, onActiveChange }: {
  disabled: boolean;
  onTranscript: (text: string) => void;
  onActiveChange: (active: boolean) => void;
}) {
  const [open, setOpen] = useState(false);
  const [state, setState] = useState<VoiceState>("idle");
  const [seconds, setSeconds] = useState(0);
  const [transcript, setTranscript] = useState("");
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const recognition = useRef<Recognition | null>(null);
  const timer = useRef<number | undefined>(undefined);
  const finalText = useRef("");
  const stopping = useRef(false);
  const callbacks = useRef({ onTranscript, onActiveChange });
  callbacks.current = { onTranscript, onActiveChange };
  const host = typeof window === "undefined" ? undefined : window as unknown as VoiceWindow;
  const Constructor = host?.SpeechRecognition ?? host?.webkitSpeechRecognition;

  // Detach handlers before abort: late platform callbacks must never edit the next task's draft.
  useEffect(() => () => {
    window.clearInterval(timer.current);
    const current = recognition.current;
    recognition.current = null;
    if (current) {
      current.onstart = current.onend = current.onerror = current.onresult = null;
      current.abort();
    }
  }, []);

  const finish = (commit: boolean) => {
    window.clearInterval(timer.current);
    timer.current = undefined;
    const current = recognition.current;
    recognition.current = null;
    if (current) current.onstart = current.onend = current.onerror = current.onresult = null;
    if (commit && finalText.current.trim()) {
      callbacks.current.onTranscript(finalText.current.trim());
      setNotice("Dictation added to your draft. Review and edit it before sending.");
    }
    finalText.current = "";
    setTranscript("");
    setState("idle");
    callbacks.current.onActiveChange(false);
    return current;
  };

  const start = () => {
    if (!Constructor || disabled || recognition.current) return;
    setError("");
    setNotice("");
    setSeconds(0);
    setTranscript("");
    finalText.current = "";
    stopping.current = false;
    setOpen(false);
    try {
      const current = new Constructor();
      recognition.current = current;
      current.lang = navigator.language;
      current.continuous = true;
      current.interimResults = true;
      current.onstart = () => {
        if (recognition.current !== current || stopping.current) return;
        setState("recording");
        const started = Date.now();
        timer.current = window.setInterval(() => setSeconds(Math.floor((Date.now() - started) / 1000)), 1000);
      };
      current.onresult = ({ results }) => {
        if (recognition.current !== current) return;
        const final: string[] = [];
        const partial: string[] = [];
        for (let index = 0; index < results.length; index++) {
          const result = results[index];
          if (!result) continue;
          const text = result[0]?.transcript ?? "";
          (result.isFinal ? final : partial).push(text);
        }
        finalText.current = final.join(" ");
        setTranscript([...final, ...partial].join(" "));
      };
      current.onerror = ({ error: code }) => {
        if (recognition.current !== current) return;
        setError(speechErrors[code] ?? `Speech recognition failed (${code}). You can still type your message.`);
        finish(true)?.abort();
      };
      current.onend = () => {
        if (recognition.current !== current) return;
        if (!finalText.current.trim()) setNotice("No final speech was recognized. Your draft is unchanged.");
        finish(true);
      };
      setState("starting");
      callbacks.current.onActiveChange(true);
      current.start();
    } catch (failure) {
      finish(false)?.abort();
      setError(`Could not start dictation: ${errText(failure)}. Type your message instead.`);
    }
  };

  const stop = () => {
    if (!recognition.current) return;
    stopping.current = true;
    setState("finishing");
    window.clearInterval(timer.current);
    timer.current = undefined;
    try { recognition.current.stop(); }
    catch (failure) {
      finish(true)?.abort();
      setError(`Could not finish dictation: ${errText(failure)}`);
    }
  };

  return <div className="flex min-w-0 flex-col gap-2">
    {state === "idle" ? <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger render={<Button type="button" size="sm" variant="ghost" disabled={disabled} aria-label="Dictate a message" />}><MicIcon aria-hidden data-icon="inline-start" />Dictate</PopoverTrigger>
      <PopoverPopup side="top" align="start" className="w-80">
        <div className="flex flex-col gap-3">
          <PopoverTitle>Dictate a message</PopoverTitle>
          {Constructor ? <>
            <p className="text-sm">Your browser or operating system may send microphone audio to its cloud speech service. Dictation is not guaranteed to stay on this device or work offline.</p>
            <p className="text-xs text-muted-foreground">Start only if you are comfortable with that processing. Stop keeps recognized text in an editable draft; Cancel discards this recording. Nothing sends to the agent automatically.</p>
            <Button type="button" size="sm" onClick={start}>Start dictation</Button>
          </> : <p role="status" className="text-sm">Speech recognition is unavailable in this browser or desktop WebView. You can type your message instead.</p>}
        </div>
      </PopoverPopup>
    </Popover> : <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <MicIcon aria-hidden className="size-4 text-warning" />
        <span role="status" className="text-xs">{state === "starting" ? "Starting microphone…" : state === "recording" ? "Listening" : "Finishing dictation…"}</span>
        <span aria-label="Dictation elapsed time" className="font-mono text-xs tabular-nums">{Math.floor(seconds / 60)}:{String(seconds % 60).padStart(2, "0")}</span>
        <Button type="button" size="xs" variant="outline" onClick={stop} disabled={state === "finishing"}><SquareIcon aria-hidden data-icon="inline-start" />Stop</Button>
        <Button type="button" size="xs" variant="ghost" onClick={() => { finish(false)?.abort(); setNotice("Dictation cancelled. Your draft is unchanged."); }}>Cancel dictation</Button>
      </div>
      {transcript && <p aria-label="Live transcript" className="max-h-24 overflow-y-auto whitespace-pre-wrap break-words text-xs text-muted-foreground">{transcript}</p>}
    </div>}
    {notice && <p role="status" className="text-xs text-muted-foreground">{notice}</p>}
    {error && <p role="alert" className="text-xs text-destructive-foreground">{error}</p>}
  </div>;
}
