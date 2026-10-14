import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({
  status: vi.fn(), setSecrets: vi.fn(), generateRelaySecrets: vi.fn(), configure: vi.fn(),
  startVerification: vi.fn(), confirmVerification: vi.fn(), voices: vi.fn(), models: vi.fn(),
  changed: [] as ((v: unknown) => void)[],
}));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({
  Jarvis: {
    status: api.status, setSecrets: api.setSecrets, generateRelaySecrets: api.generateRelaySecrets, configure: api.configure,
    startVerification: api.startVerification, confirmVerification: api.confirmVerification, voices: api.voices, models: api.models,
  },
  events: { jarvisChanged: { on: (cb: (v: unknown) => void) => { api.changed.push(cb); return () => undefined; } } },
}));

import { PhoneSettings } from "./PhoneSettings";
import { useJarvis } from "./store";

const empty = {
  settings: {}, hasTwilio: false, hasElevenLabs: false, hasRelaySecrets: false, hasWebhookSecret: false,
  relayConnected: false, verificationPending: false, callsToday: 0, monthCostMicros: 0,
};
const ready = { ...empty, hasTwilio: true, hasElevenLabs: true, hasRelaySecrets: true, relayConnected: true, settings: { agentId: "ag" } };

beforeEach(() => {
  vi.clearAllMocks();
  api.changed.length = 0;
  useJarvis.setState({ status: null, error: null });
  api.voices.mockResolvedValue([]);
  api.models.mockResolvedValue([]);
});
afterEach(cleanup);

it("shows the setup checklist with nothing configured", async () => {
  api.status.mockResolvedValue(empty);
  render(<PhoneSettings />);
  expect(await screen.findByText("Twilio account")).toBeInTheDocument();
  expect(screen.getByText("Relay connected")).toBeInTheDocument();
  expect(screen.getByText("Webhook secret (optional)")).toBeInTheDocument();
});

it("does not ask ElevenLabs for voices until a key is stored", async () => {
  api.status.mockResolvedValue(empty);
  render(<PhoneSettings />);
  expect(await screen.findByText("Twilio account")).toBeInTheDocument();
  expect(api.voices).not.toHaveBeenCalled();
  expect(api.models).not.toHaveBeenCalled();
  act(() => { api.changed.forEach((cb) => cb({ ...empty, hasElevenLabs: true })); });
  await waitFor(() => expect(api.voices).toHaveBeenCalled());
  expect(api.models).toHaveBeenCalled();
});

it("saves Twilio secrets without echoing them back", async () => {
  api.status.mockResolvedValue(empty);
  api.setSecrets.mockResolvedValue({ ...empty, hasTwilio: true });
  render(<PhoneSettings />);
  fireEvent.change(await screen.findByLabelText("Account SID"), { target: { value: "AC123" } });
  fireEvent.change(screen.getByLabelText("Auth token"), { target: { value: "tok" } });
  fireEvent.click(screen.getByRole("button", { name: "Save Twilio" }));
  await waitFor(() => expect(api.setSecrets).toHaveBeenCalledWith({ twilioSid: "AC123", twilioToken: "tok" }));
  await waitFor(() => expect(screen.getByLabelText("Auth token")).toHaveValue(""));
  expect(screen.getByLabelText("Account SID")).toHaveValue("");
});

it("shows generated relay secrets with copy buttons", async () => {
  api.status.mockResolvedValue(empty);
  api.generateRelaySecrets.mockResolvedValue({ relayToken: "rt", llmSecret: "ls" });
  render(<PhoneSettings />);
  fireEvent.click(await screen.findByRole("button", { name: "Generate relay secrets" }));
  expect(await screen.findByText("rt")).toBeInTheDocument();
  expect(screen.getByText("ls")).toBeInTheDocument();
  expect(screen.getAllByRole("button", { name: /Copy/ })).toHaveLength(2);
  expect(screen.getByText(/secret put RELAY_TOKEN/)).toBeInTheDocument();
});

it("shows the webhook secret once after connecting", async () => {
  api.status.mockResolvedValue(ready);
  api.configure.mockResolvedValue({ status: { ...ready, hasWebhookSecret: true }, webhookSecret: "whsec_abc" });
  render(<PhoneSettings />);
  fireEvent.change(await screen.findByLabelText("Relay URL"), { target: { value: "https://r.workers.dev" } });
  fireEvent.change(screen.getByLabelText("Jarvis number"), { target: { value: "+3220000000" } });
  fireEvent.click(screen.getByRole("button", { name: "Connect" }));
  expect(await screen.findByText("whsec_abc")).toBeInTheDocument();
  expect(api.configure).toHaveBeenCalledWith(expect.objectContaining({ relayUrl: "https://r.workers.dev", twilioNumber: "+3220000000" }));
  expect(screen.getByText(/secret put WEBHOOK_SECRET/)).toBeInTheDocument();
  expect(screen.getByText(/only once/i)).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Hide webhook secret" }));
  expect(screen.queryByText("whsec_abc")).not.toBeInTheDocument();
});

it("does not show a webhook secret when none was created", async () => {
  const configured = { ...ready, settings: { ...ready.settings, relayUrl: "https://r.workers.dev", twilioNumber: "+3220000000" } };
  api.status.mockResolvedValue(configured);
  api.configure.mockResolvedValue({ status: configured });
  render(<PhoneSettings />);
  fireEvent.click(await screen.findByRole("button", { name: "Connect" }));
  await waitFor(() => expect(api.configure).toHaveBeenCalled());
  expect(screen.queryByText(/secret put WEBHOOK_SECRET/)).not.toBeInTheDocument();
});

it("verifies the owner number with a code", async () => {
  api.status.mockResolvedValue(ready);
  api.startVerification.mockResolvedValue(undefined);
  api.confirmVerification.mockResolvedValue({ ...empty, settings: { ownerNumber: "+32470000000", ownerVerified: true } });
  render(<PhoneSettings />);
  fireEvent.change(await screen.findByLabelText("Your phone number"), { target: { value: "+32470000000" } });
  fireEvent.click(screen.getByRole("button", { name: "Call me with a code" }));
  await waitFor(() => expect(api.startVerification).toHaveBeenCalledWith("+32470000000"));
  fireEvent.change(await screen.findByLabelText("Code"), { target: { value: "123456" } });
  fireEvent.click(screen.getByRole("button", { name: "Verify" }));
  await waitFor(() => expect(api.confirmVerification).toHaveBeenCalledWith("123456"));
});

it("updates live from the jarvis changed event", async () => {
  api.status.mockResolvedValue(empty);
  render(<PhoneSettings />);
  await screen.findByText("Twilio account");
  act(() => api.changed.forEach((cb) => cb({ ...empty, relayConnected: true, callsToday: 3 })));
  expect(await screen.findByText(/Calls today: 3/)).toBeInTheDocument();
});

it("shows a daemon error", async () => {
  api.status.mockRejectedValue(new Error("daemon offline"));
  render(<PhoneSettings />);
  expect((await screen.findAllByRole("alert"))[0]).toHaveTextContent("daemon offline");
});

it("saves the call language, Dutch unless chosen otherwise", async () => {
  api.status.mockResolvedValue(ready);
  api.configure.mockResolvedValue({ status: ready });
  render(<PhoneSettings />);
  const language = await screen.findByRole("combobox", { name: "Call language" });
  expect(language).toHaveTextContent("Nederlands");
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(api.configure).toHaveBeenCalledWith(expect.objectContaining({ callLanguage: "nl" })));
});
