import { create } from "zustand";
import { Dictation, events, type VoiceStatus } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { useDaemon } from "@/stores/daemon";
import type { Outcome } from "@/stores/llm";

/** What is in flight for a model id, or for "language". */
export type DictationAction = "download" | "cancel" | "remove" | "use" | "language";

/** The last failed action, keyed like `busy` so the row that started it can show it. */
export interface ActionError {
  target: string;
  message: string;
}

interface State {
  status: VoiceStatus | null;
  loading: boolean;
  /** Why the status could not be loaded (user-ready). */
  error: string | null;
  busy: Record<string, DictationAction>;
  actionError: ActionError | null;
  load: () => Promise<void>;
  download: (id: string) => Promise<Outcome<VoiceStatus>>;
  cancel: (id: string) => Promise<Outcome<VoiceStatus>>;
  remove: (id: string) => Promise<Outcome<VoiceStatus>>;
  /** Makes a downloaded model the one dictation uses (`Dictation.use`). */
  choose: (id: string) => Promise<Outcome<VoiceStatus>>;
  setLanguage: (language: string) => Promise<Outcome<VoiceStatus>>;
  startSync: () => () => void;
}

const IPC_CODE = /^(bad_request|not_found|conflict|canceled|timeout|internal|denied): /;

/**
 * The daemon's dictation errors arrive as "dictation_<code>: <sentence>" and the sentence is user-ready. Anything
 * else (daemon offline, IPC failure) has `code` "" and loses its machine prefix.
 */
export function dictationError(failure: unknown): { code: string; message: string } {
  const text = failure instanceof Error ? failure.message : String(failure);
  const match = /^dictation_([a-z_]+): ([\s\S]*)$/.exec(text);
  if (match) return { code: match[1]!, message: match[2]! };
  return { code: "", message: text.replace(IPC_CODE, "") };
}

/** Bytes as the Voice settings show them: MiB to one decimal, labelled MB. */
export const megabytes = (bytes: number) => (bytes / 1048576).toFixed(1);

// Every status that lands (event or action reply) bumps the revision; a load only applies if none landed meanwhile.
let revision = 0;
let subscribers = 0;
let stop: (() => void) | undefined;

/** The daemon owns the models and their downloads. This is an event-synchronized snapshot of its VoiceStatus. */
export const useDictation = create<State>((set, get) => {
  const apply = (status: VoiceStatus) => {
    revision++;
    set({ status, error: null });
  };

  async function act(target: string, kind: DictationAction, run: () => Promise<VoiceStatus>): Promise<Outcome<VoiceStatus>> {
    set((state) => ({ busy: { ...state.busy, [target]: kind }, actionError: state.actionError?.target === target ? null : state.actionError }));
    try {
      const status = await run();
      apply(status);
      return { ok: true, value: status };
    } catch (failure) {
      const { code, message } = dictationError(failure);
      set({ actionError: { target, message } });
      return { ok: false, error: message, code };
    } finally {
      set((state) => {
        const busy = { ...state.busy };
        delete busy[target];
        return { busy };
      });
    }
  }

  return {
    status: null, loading: false, error: null, busy: {}, actionError: null,

    async load() {
      if (!inDesktop) return;
      const started = revision;
      set({ loading: true, error: null });
      try {
        const status = await Dictation.status();
        if (revision === started) apply(status);
        set({ loading: false });
      } catch (failure) {
        set({ loading: false, error: dictationError(failure).message });
      }
    },

    download: (id) => act(id, "download", () => Dictation.download({ id })),
    cancel: (id) => act(id, "cancel", () => Dictation.cancel({ id })),
    remove: (id) => act(id, "remove", () => Dictation.remove({ id })),
    choose: (id) => act(id, "use", () => Dictation.use({ id })),
    setLanguage: (language) => act("language", "language", () => Dictation.setLanguage({ language })),

    startSync() {
      if (!inDesktop) return () => {};
      if (++subscribers === 1) {
        const offStatus = events.dictationChanged.on(apply);
        // A restarted daemon may have finished, failed or dropped a download while it was away.
        const offDaemon = useDaemon.subscribe((state, previous) => {
          if (state.link === "connected" && previous.link !== "connected") void get().load();
        });
        stop = () => { offStatus(); offDaemon(); };
      }
      let stopped = false;
      return () => {
        if (stopped) return;
        stopped = true;
        if (--subscribers === 0) { stop?.(); stop = undefined; }
      };
    },
  };
});
