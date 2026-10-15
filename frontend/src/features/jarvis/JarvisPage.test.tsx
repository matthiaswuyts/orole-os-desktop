import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({ rules: vi.fn(), deleteRule: vi.fn(), testCall: vi.fn(), voices: vi.fn(), models: vi.fn(), all: vi.fn(), openSettings: vi.fn() }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/stores/ui", () => ({ useUI: { getState: () => ({ openSettings: api.openSettings }) } }));
vi.mock("@/mygo", () => ({
  Jarvis: { rules: api.rules, deleteRule: api.deleteRule, testCall: api.testCall, voices: api.voices, models: api.models, status: vi.fn() },
  Settings: { all: api.all },
  events: { jarvisChanged: { on: () => () => undefined } },
}));

import { initialForm } from "@/features/chat/builder/botForm";
import { JarvisPage } from "./JarvisPage";
import { useJarvis } from "./store";

const ready = { settings: { agentId: "ag", ownerVerified: true }, hasTwilio: true, hasElevenLabs: true, hasRelaySecrets: true, hasWebhookSecret: false, relayConnected: true, verificationPending: false, callsToday: 0, monthCostMicros: 0 };
const bot = { id: "b1", name: "Atlas" } as never;
const full = { ...initialForm(), codeTasks: true, toolAccess: "all", taskPermissionMode: "yolo" };

beforeEach(() => {
  vi.clearAllMocks();
  api.rules.mockResolvedValue([{ id: "r1", botId: "b1", trigger: "task_failed", target: "any", repeat: true, urgent: false, quietOverride: false, note: "", createdAt: 1 }]);
  api.voices.mockResolvedValue([{ id: "v1", name: "Rachel", description: "Calm" }]);
  api.models.mockResolvedValue([{ id: "eleven_flash_v2_5", name: "Flash v2.5", description: "Fast.", recommended: true }]);
  api.all.mockResolvedValue({ agents: { botYolo: true } });
  useJarvis.setState({ status: ready as never, error: null });
});
afterEach(cleanup);

it("sends users to phone setup when it is not done", async () => {
  useJarvis.setState({ status: { ...ready, settings: { ownerVerified: false } } as never });
  render(<JarvisPage bot={bot} form={full} patch={vi.fn()} />);
  const button = await screen.findByRole("button", { name: "Set up phone calls" });
  expect(screen.queryByRole("switch", { name: "Jarvis mode" })).toBeNull();
  fireEvent.click(button);
  expect(api.openSettings).toHaveBeenCalledWith("phone");
});

it("keeps the switch and rules while the relay is down; only the test call waits", async () => {
  useJarvis.setState({ status: { ...ready, relayConnected: false } as never });
  const patch = vi.fn();
  render(<JarvisPage bot={bot} form={{ ...full, jarvisEnabled: true }} patch={patch} />);
  expect(await screen.findByText("When any task I start fails")).toBeInTheDocument();
  expect(screen.getByText(/relay is not connected/)).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Test call" })).toBeDisabled();
  // Past calls are cards in the bot's chat; this page lists none.
  expect(screen.queryByText("Recent calls")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("switch", { name: "Jarvis mode" }));
  expect(patch).toHaveBeenCalledWith({ jarvisEnabled: false });
});

it("does not ask ElevenLabs for voices without a key", async () => {
  useJarvis.setState({ status: { ...ready, hasElevenLabs: false } as never });
  render(<JarvisPage bot={bot} form={full} patch={vi.fn()} />);
  expect(await screen.findByRole("switch", { name: "Jarvis mode" })).toBeInTheDocument();
  expect(api.voices).not.toHaveBeenCalled();
  expect(api.models).not.toHaveBeenCalled();
});

const voiceList = { language: "nl", library: [], groups: [{ label: "Vlaams", voices: [{ id: "v1", name: "Rachel", description: "Calm", inLibrary: true }] }] };

it("reopens a stored voice id the lists do not know as a custom id and edits it", async () => {
  api.voices.mockResolvedValue(voiceList);
  const patch = vi.fn();
  render(<JarvisPage bot={bot} form={{ ...full, jarvisVoice: "myClone123" }} patch={patch} />);
  const field = await screen.findByRole("textbox", { name: "ElevenLabs voice ID" });
  expect(field).toHaveValue("myClone123");
  expect(screen.getAllByRole("combobox")[0]).toHaveTextContent("Custom voice ID");
  fireEvent.change(field, { target: { value: " abc 123 " } });
  expect(patch).toHaveBeenLastCalledWith({ jarvisVoice: "abc123" });
});

async function pick(name: RegExp) {
  const option = await screen.findByRole("option", { name });
  fireEvent.mouseMove(option);
  fireEvent.mouseUp(option);
  fireEvent.click(option);
}

it("shows the custom id field only after Custom voice ID is picked, and hides it for a listed voice", async () => {
  api.voices.mockResolvedValue(voiceList);
  const patch = vi.fn();
  render(<JarvisPage bot={bot} form={{ ...full, jarvisVoice: "v1" }} patch={patch} />);
  const select = await screen.findByRole("combobox", { name: "Voice" });
  await waitFor(() => expect(select).toHaveTextContent("Rachel"));
  expect(screen.queryByRole("textbox", { name: "ElevenLabs voice ID" })).toBeNull();
  fireEvent.click(select);
  await pick(/Custom voice ID/);
  const field = await screen.findByRole("textbox", { name: "ElevenLabs voice ID" });
  expect(field).toHaveValue("");
  expect(patch).not.toHaveBeenCalled();
  fireEvent.click(select);
  await pick(/Rachel/);
  await waitFor(() => expect(screen.queryByRole("textbox", { name: "ElevenLabs voice ID" })).toBeNull());
  expect(patch).toHaveBeenLastCalledWith({ jarvisVoice: "v1" });
});

it("disables the toggle and says why without full permissions", async () => {
  render(<JarvisPage bot={bot} form={{ ...full, toolAccess: "write" }} patch={vi.fn()} />);
  const toggle = await screen.findByRole("switch", { name: "Jarvis mode" });
  expect(toggle).toHaveAttribute("aria-disabled", "true");
  expect(screen.getByText(/tool access set to All/)).toBeInTheDocument();
});

it("keeps the toggle usable to switch Jarvis off while permissions are missing", async () => {
  const patch = vi.fn();
  render(<JarvisPage bot={bot} form={{ ...full, toolAccess: "write", jarvisEnabled: true }} patch={patch} />);
  fireEvent.click(await screen.findByRole("switch", { name: "Jarvis mode" }));
  expect(patch).toHaveBeenCalledWith({ jarvisEnabled: false });
});

it("turns Jarvis on and edits the policy", async () => {
  const patch = vi.fn();
  render(<JarvisPage bot={bot} form={full} patch={patch} />);
  fireEvent.click(await screen.findByRole("switch", { name: "Jarvis mode" }));
  expect(patch).toHaveBeenCalledWith({ jarvisEnabled: true });
  fireEvent.change(screen.getByLabelText("When to call me"), { target: { value: "Only failures" } });
  expect(patch).toHaveBeenCalledWith({ jarvisPolicy: "Only failures" });
});

it("lists and deletes rules", async () => {
  api.deleteRule.mockResolvedValue(undefined);
  render(<JarvisPage bot={bot} form={{ ...full, jarvisEnabled: true }} patch={vi.fn()} />);
  expect(await screen.findByText("When any task I start fails")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Delete rule: When any task I start fails" }));
  await waitFor(() => expect(api.deleteRule).toHaveBeenCalledWith("r1"));
  await waitFor(() => expect(screen.queryByText("When any task I start fails")).toBeNull());
});

it("places a test call and shows the answer", async () => {
  api.testCall.mockResolvedValue("Calling your phone.");
  render(<JarvisPage bot={bot} form={{ ...full, jarvisEnabled: true }} patch={vi.fn()} />);
  fireEvent.click(await screen.findByRole("button", { name: "Test call" }));
  expect(await screen.findByText("Calling your phone.")).toBeInTheDocument();
  expect(api.testCall).toHaveBeenCalledWith("b1");
});

it("keeps the test call off until Jarvis is on", async () => {
  render(<JarvisPage bot={bot} form={full} patch={vi.fn()} />);
  expect(await screen.findByRole("button", { name: "Test call" })).toBeDisabled();
});
