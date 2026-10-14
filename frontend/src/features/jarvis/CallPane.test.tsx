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
vi.mock("@/lib/backend", () => ({ inDesktop: true }));

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
  useChat.setState({ bots: [{ id: "b1", name: "Atlas" } as never], pane: { kind: "call", id: "c1" }, settingsOpen: false });
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
