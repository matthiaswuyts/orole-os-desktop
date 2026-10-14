import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { CallTranscript, Message, MessageDelta } from "@/mygo";

const bus = vi.hoisted(() => {
  const handlers = new Map<string, Set<(payload: unknown) => void>>();
  return {
    handlers,
    emit: (name: string, payload: unknown) => handlers.get(name)?.forEach((h) => h(payload)),
    event: (name: string) => ({ on: (h: (payload: unknown) => void) => {
      if (!handlers.has(name)) handlers.set(name, new Set());
      handlers.get(name)!.add(h);
      return () => { handlers.get(name)!.delete(h); };
    } }),
  };
});
const api = vi.hoisted(() => ({ transcript: vi.fn() }));
vi.mock("@/mygo", () => ({
  Jarvis: api,
  events: new Proxy({}, { get: (_target, name: string) => bus.event(name) }),
}));
const backend = vi.hoisted(() => ({ inDesktop: true }));
vi.mock("@/lib/backend", () => backend);

import { useChat } from "@/stores/chat";
import { SidePane } from "@/features/chat/SidePane";
import { CallCard } from "./CallCard";
import { CallPane } from "./CallPane";

const msg = (id: string, seq: number, role: "user" | "assistant", parts: Message["parts"], status = "complete"): Message =>
  ({ id, botId: "b1", seq, role, author: role === "user" ? "user" : "bot:b1", parts, status, createdAt: seq, callId: "c1" });

const transcript = (): CallTranscript => ({
  call: { id: "c1", botId: "b1", direction: "outbound", reason: "The deploy failed", status: "completed", startedAt: 1_700_000_000_000,
    answeredAt: 1_700_000_001_000, endedAt: 1_700_000_076_000, estCostMicros: 150_000, createdAt: 1_700_000_000_000 },
  messages: [
    msg("u1", 1, "user", [{ type: "text", text: "what happened?" }]),
    msg("a1", 2, "assistant", [{ type: "tool", id: "x", name: "web_search", status: "done" }, { type: "task", id: "t9", title: "Fix the deploy", name: "claude" }, { type: "text", text: "The deploy failed on a test." }]),
  ],
});

beforeEach(() => {
  vi.clearAllMocks();
  bus.handlers.clear();
  backend.inDesktop = true;
  useChat.setState({ bots: [{ id: "b1", name: "Atlas" } as never, { id: "b2", name: "Nova" } as never], pane: { kind: "call", id: "c1" }, settingsOpen: false });
  api.transcript.mockResolvedValue(transcript());
});
afterEach(cleanup);

it("shows the call header and the transcript in order", async () => {
  render(<CallPane callId="c1" botId="b1" onClose={vi.fn()} />);
  expect(await screen.findByRole("heading", { name: "Called you · Atlas" })).toBeInTheDocument();
  expect(api.transcript).toHaveBeenCalledWith("c1");
  const meta = screen.getByText("Duration").closest("dl")!;
  expect(within(meta).getByText("1:15")).toBeInTheDocument();
  expect(within(meta).getByText("est. $0.15")).toBeInTheDocument();
  expect(within(meta).getByText("Ended")).toBeInTheDocument();
  expect(screen.getByText("The deploy failed")).toBeInTheDocument();
  const items = within(screen.getByRole("list", { name: "Transcript" })).getAllByRole("listitem");
  expect(items).toHaveLength(2);
  expect(items[0]).toHaveTextContent("what happened?");
  expect(items[1]).toHaveTextContent("Searched the web");
  expect(items[1]).toHaveTextContent("The deploy failed on a test.");
});

it("opens a task the bot started on the call", async () => {
  render(<CallPane callId="c1" botId="b1" onClose={vi.fn()} />);
  fireEvent.click(await screen.findByRole("button", { name: /Code task: Fix the deploy/ }));
  expect(useChat.getState().pane).toEqual({ kind: "task", id: "t9" });
});

it("appends what is said while the call is live and streams the reply", async () => {
  render(<CallPane callId="c1" botId="b1" onClose={vi.fn()} />);
  await screen.findByRole("list", { name: "Transcript" });
  act(() => {
    bus.emit("chatCallMessage", msg("u2", 3, "user", [{ type: "text", text: "and now?" }]));
    bus.emit("chatCallMessage", { ...msg("other", 9, "user", [{ type: "text", text: "another call" }]), callId: "c2" });
    bus.emit("chatCallMessage", msg("a2", 4, "assistant", [{ type: "text", text: "Fix" }], "running"));
    bus.emit("chatCallDelta", { botId: "b1", messageId: "a2", part: 0, type: "text", from: 3, text: "ing it now." } satisfies MessageDelta);
  });
  const items = within(screen.getByRole("list", { name: "Transcript" })).getAllByRole("listitem");
  expect(items).toHaveLength(4);
  expect(items[2]).toHaveTextContent("and now?");
  expect(items[3]).toHaveTextContent("Fixing it now.");
  expect(screen.queryByText("another call")).not.toBeInTheDocument();
});

it("refreshes the header when the call's card changes", async () => {
  const live = transcript();
  live.call = { ...live.call, status: "in_progress", endedAt: 0, estCostMicros: 0 };
  api.transcript.mockResolvedValueOnce(live);
  render(<CallPane callId="c1" botId="b1" onClose={vi.fn()} />);
  expect(await screen.findByRole("heading", { name: "Calling you… · Atlas" })).toBeInTheDocument();
  act(() => bus.emit("chatMessage", { ...msg("card", 1, "assistant", [{ type: "call", id: "c1", status: "completed" }]), callId: undefined }));
  await waitFor(() => expect(screen.getByRole("heading", { name: "Called you · Atlas" })).toBeInTheDocument());
});

it("opens beside the conversation when the call card is clicked", async () => {
  useChat.setState({ pane: null });
  const container = { current: document.createElement("div") };
  render(<><CallCard part={{ type: "call", id: "c1", name: "outbound", status: "completed" }} /><SidePane botId="b1" container={container} /></>);
  expect(document.querySelector("[data-slot=call-pane]")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: /Called you/ }));
  expect(await screen.findByRole("heading", { name: "Called you · Atlas" })).toBeInTheDocument();
  expect(document.querySelector("[data-slot=side-pane] [data-slot=call-pane]")).not.toBeNull();
});

it("names the bot of each line after a hand-over and keeps the starting bot in the header", async () => {
  const t = transcript();
  t.messages.push(msg("u2", 3, "user", [{ type: "text", text: "put me through to Nova" }]),
    { ...msg("a2", 4, "assistant", [{ type: "text", text: "Nova here." }]), botId: "b2", author: "bot:b2" });
  api.transcript.mockResolvedValueOnce(t);
  render(<CallPane callId="c1" botId="b1" onClose={vi.fn()} />);
  expect(await screen.findByRole("heading", { name: "Called you · Atlas" })).toBeInTheDocument();
  const items = within(screen.getByRole("list", { name: "Transcript" })).getAllByRole("listitem");
  expect(items[1]).toHaveTextContent(/^Atlas/);
  expect(within(items[1]!).queryByText(/Handed to/)).toBeNull();
  expect(items[3]).toHaveTextContent("Handed to Nova");
  expect(items[3]).toHaveTextContent("Nova here.");
});

it("announces only the live state, not the transcript", async () => {
  const live = transcript();
  live.call = { ...live.call, status: "in_progress", endedAt: 0 };
  api.transcript.mockResolvedValueOnce(live);
  render(<CallPane callId="c1" botId="b1" onClose={vi.fn()} />);
  await screen.findByRole("list", { name: "Transcript" });
  expect(screen.getByRole("status")).toHaveTextContent("Call in progress");
  expect(document.querySelector("[aria-live]")).toBeNull();
  act(() => bus.emit("chatCallMessage", msg("a9", 9, "assistant", [], "running")));
  expect(screen.getByRole("status")).toHaveTextContent("Atlas is answering");
});

const from = (author: string, id: string, seq: number, text: string): Message => ({ ...msg(id, seq, "user", [{ type: "text", text }]), author });

it("draws a bot's report as the collapsed reported-back card and opens it on click", async () => {
  const t = transcript();
  t.messages.push(from("report:b2", "r1", 3, "Nova found two failing tests."));
  api.transcript.mockResolvedValueOnce(t);
  render(<CallPane callId="c1" botId="b1" onClose={vi.fn()} />);
  expect(await screen.findByText("Nova reported back")).toBeInTheDocument();
  const card = document.querySelector("[data-slot=chat-report]") as HTMLDetailsElement;
  expect(card.open).toBe(false);
  expect(card).toHaveTextContent("Nova found two failing tests.");
  expect(document.querySelector("[data-slot=chat-user]")).toHaveTextContent("what happened?");
  expect(document.querySelectorAll("[data-slot=chat-user]")).toHaveLength(1);
  fireEvent.click(within(card).getByText("Nova reported back"));
  expect(card.open).toBe(true);
});

it("names an unknown reporting bot generically", async () => {
  const t = transcript();
  t.messages.push(from("report:gone", "r1", 3, "Done."));
  api.transcript.mockResolvedValueOnce(t);
  render(<CallPane callId="c1" botId="b1" onClose={vi.fn()} />);
  expect(await screen.findByText("Another bot reported back")).toBeInTheDocument();
});

it("shows a code task update collapsed and opens its task", async () => {
  const t = transcript();
  t.messages.push(from("task:t42", "k1", 3, "The task finished: 12 tests pass."));
  api.transcript.mockResolvedValueOnce(t);
  render(<CallPane callId="c1" botId="b1" onClose={vi.fn()} />);
  const update = (await screen.findByText("Code task update")).closest("[data-slot=call-task-update]") as HTMLElement;
  const details = update.querySelector("details")!;
  expect(details.open).toBe(false);
  expect(update).toHaveTextContent("The task finished: 12 tests pass.");
  expect(update.closest("[data-slot=chat-user]")).toBeNull();
  fireEvent.click(within(update).getByText("Code task update"));
  expect(details.open).toBe(true);
  fireEvent.click(within(update).getByRole("button", { name: "Open the task" }));
  expect(useChat.getState().pane).toEqual({ kind: "task", id: "t42" });
});

it("offers no task button when the task update names no task", async () => {
  const t = transcript();
  t.messages.push(from("task:", "k1", 3, "An update."));
  api.transcript.mockResolvedValueOnce(t);
  render(<CallPane callId="c1" botId="b1" onClose={vi.fn()} />);
  expect(await screen.findByText("Code task update")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Open the task" })).toBeNull();
});

it("draws cues as nothing and any other author as a collapsed item with its label", async () => {
  const t = transcript();
  t.messages.push(from("cue", "q1", 3, "Tell the owner the build is done."), from("automation:a1", "x1", 4, "Nightly backup finished."), from("", "e1", 5, "Plain words."));
  api.transcript.mockResolvedValueOnce(t);
  render(<CallPane callId="c1" botId="b1" onClose={vi.fn()} />);
  expect(await screen.findByText("Message from automation:a1")).toBeInTheDocument();
  expect(screen.queryByText(/Tell the owner/)).toBeNull();
  const generic = document.querySelector("[data-slot=call-followup]")!;
  expect(generic.querySelector("details")!.open).toBe(false);
  expect(generic).toHaveTextContent("Nightly backup finished.");
  // An empty author is the owner's own line.
  expect(screen.getByText("Plain words.").closest("[data-slot=chat-user]")).not.toBeNull();
  // The cue gets no (empty) list item: the owner's line, the bot's, the automation's and the plain words remain.
  expect(within(screen.getByRole("list", { name: "Transcript" })).getAllByRole("listitem")).toHaveLength(4);
});

it("shows follow-ups that arrive after the call ended and keeps the call ended", async () => {
  render(<CallPane callId="c1" botId="b1" onClose={vi.fn()} />);
  await screen.findByRole("list", { name: "Transcript" });
  const meta = screen.getByText("Duration").closest("dl")!;
  act(() => {
    bus.emit("chatCallMessage", from("report:b2", "r1", 3, "Nova is done."));
    bus.emit("chatCallMessage", msg("a2", 4, "assistant", [{ type: "text", text: "Nova says it is done." }], "running"));
  });
  expect(screen.getByText("Nova reported back")).toBeInTheDocument();
  expect(screen.getByText("Nova says it is done.")).toBeInTheDocument();
  expect(within(meta).getByText("Ended")).toBeInTheDocument();
  expect(screen.getByRole("heading", { name: "Called you · Atlas" })).toBeInTheDocument();
  // The call is over, so an answer being written is not announced as the call going on.
  expect(screen.getByRole("status")).toHaveTextContent("");
  act(() => bus.emit("chatCallMessage", msg("a2", 4, "assistant", [{ type: "text", text: "Nova says it is done." }])));
  expect(within(meta).getByText("Ended")).toBeInTheDocument();
});

it("says the transcript needs the desktop app outside it", () => {
  backend.inDesktop = false;
  render(<CallPane callId="c1" botId="b1" onClose={vi.fn()} />);
  expect(screen.getByText("Call transcripts are available in the desktop app.")).toBeInTheDocument();
  expect(api.transcript).not.toHaveBeenCalled();
});
