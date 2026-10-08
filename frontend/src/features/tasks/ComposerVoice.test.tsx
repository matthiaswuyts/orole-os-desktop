import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { Session } from "@/mygo";

const api = vi.hoisted(() => ({ prompt: vi.fn(), queuePrompt: vi.fn() }));
vi.mock("@/mygo", () => ({ Agents: api }));
vi.mock("@/stores/agents", () => ({ useAgents: { getState: () => ({ upsert: vi.fn() }) } }));
vi.mock("@/features/agents/shared", () => ({ ErrorNote: () => null }));
import { Composer } from "./Composer";

class SpeechRecognitionDouble {
  static instances: SpeechRecognitionDouble[] = [];
  lang = "";
  continuous = false;
  interimResults = false;
  onstart: (() => void) | null = null;
  onend: (() => void) | null = null;
  onerror: ((event: { error: string }) => void) | null = null;
  onresult: ((event: { results: { isFinal: boolean; 0: { transcript: string } }[] }) => void) | null = null;
  start = vi.fn(() => this.onstart?.());
  stop = vi.fn();
  abort = vi.fn();
  constructor() { SpeechRecognitionDouble.instances.push(this); }
}
const task = { id: "voice-task", live: true, status: "idle", mode: "structured", machine: "local", pending: [], questions: [], queue: [] } as unknown as Session;

beforeEach(() => {
  vi.clearAllMocks();
  SpeechRecognitionDouble.instances = [];
  vi.stubGlobal("SpeechRecognition", SpeechRecognitionDouble);
  vi.stubGlobal("webkitSpeechRecognition", undefined);
});
afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); });

it("requires disclosed consent, then stops into an editable draft without sending", () => {
  render(<Composer session={task} />);
  fireEvent.change(screen.getByLabelText("Message the agent"), { target: { value: "Existing draft." } });
  fireEvent.click(screen.getByRole("button", { name: "Dictate a message" }));
  expect(screen.getByText(/may send microphone audio to its cloud speech service/)).toBeInTheDocument();
  expect(SpeechRecognitionDouble.instances).toHaveLength(0);
  vi.useFakeTimers();
  fireEvent.click(screen.getByRole("button", { name: "Start dictation" }));
  const speech = SpeechRecognitionDouble.instances[0]!;
  expect(speech.start).toHaveBeenCalledOnce();
  expect(screen.getByRole("button", { name: "Send" })).toBeDisabled();
  act(() => { vi.advanceTimersByTime(2100); });
  expect(screen.getByLabelText("Dictation elapsed time")).toHaveTextContent("0:02");
  act(() => speech.onresult?.({ results: [{ isFinal: true, 0: { transcript: "Recognized sentence." } }, { isFinal: false, 0: { transcript: "unfinished" } }] }));
  expect(screen.getByLabelText("Live transcript")).toHaveTextContent("Recognized sentence. unfinished");
  fireEvent.click(screen.getByRole("button", { name: "Stop" }));
  expect(speech.stop).toHaveBeenCalledOnce();
  act(() => speech.onend?.());
  expect(screen.getByLabelText("Message the agent")).toHaveValue("Existing draft. Recognized sentence.");
  expect(screen.getByLabelText("Message the agent")).toBeEnabled();
  fireEvent.change(screen.getByLabelText("Message the agent"), { target: { value: "Edited transcript" } });
  expect(api.prompt).not.toHaveBeenCalled();
  expect(api.queuePrompt).not.toHaveBeenCalled();
});

it("cancels captured text and ignores callbacks after navigation", () => {
  const view = render(<Composer session={task} />);
  fireEvent.click(screen.getByRole("button", { name: "Dictate a message" }));
  fireEvent.click(screen.getByRole("button", { name: "Start dictation" }));
  const first = SpeechRecognitionDouble.instances[0]!;
  act(() => first.onresult?.({ results: [{ isFinal: true, 0: { transcript: "Discard this" } }] }));
  fireEvent.click(screen.getByRole("button", { name: "Cancel dictation" }));
  expect(first.abort).toHaveBeenCalledOnce();
  expect(screen.getByLabelText("Message the agent")).toHaveValue("");
  fireEvent.click(screen.getByRole("button", { name: "Dictate a message" }));
  fireEvent.click(screen.getByRole("button", { name: "Start dictation" }));
  const second = SpeechRecognitionDouble.instances[1]!;
  const lateResult = second.onresult;
  const lateEnd = second.onend;
  view.rerender(<Composer session={{ ...task, id: "other-task" }} />);
  expect(second.abort).toHaveBeenCalledOnce();
  expect(second.onresult).toBeNull();
  act(() => { lateResult?.({ results: [{ isFinal: true, 0: { transcript: "Wrong task" } }] }); lateEnd?.(); });
  expect(screen.getByLabelText("Message the agent")).toHaveValue("");
});

it("reports unsupported platforms honestly without offering a fake recording", () => {
  vi.stubGlobal("SpeechRecognition", undefined);
  render(<Composer session={task} />);
  fireEvent.click(screen.getByRole("button", { name: "Dictate a message" }));
  expect(screen.getByRole("status")).toHaveTextContent("Speech recognition is unavailable");
  expect(screen.queryByRole("button", { name: "Start dictation" })).not.toBeInTheDocument();
  expect(SpeechRecognitionDouble.instances).toHaveLength(0);
});

it("recovers from recognition errors without losing confirmed speech or the existing draft", () => {
  render(<Composer session={task} />);
  fireEvent.change(screen.getByLabelText("Message the agent"), { target: { value: "Keep this" } });
  fireEvent.click(screen.getByRole("button", { name: "Dictate a message" }));
  fireEvent.click(screen.getByRole("button", { name: "Start dictation" }));
  const speech = SpeechRecognitionDouble.instances[0]!;
  act(() => speech.onresult?.({ results: [{ isFinal: true, 0: { transcript: "and this" } }] }));
  act(() => speech.onerror?.({ error: "network" }));
  expect(screen.getByRole("alert")).toHaveTextContent("could not reach its service");
  expect(screen.getByLabelText("Message the agent")).toHaveValue("Keep this and this");
  expect(screen.getByLabelText("Message the agent")).toBeEnabled();
  expect(speech.abort).toHaveBeenCalledOnce();
});
