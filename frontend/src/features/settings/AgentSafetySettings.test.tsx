import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({ all: vi.fn(), set: vi.fn(), rules: vi.fn(), deleteRule: vi.fn() }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({ Settings: { all: api.all, set: api.set }, Policy: { rules: api.rules, deleteRule: api.deleteRule } }));
import { AgentSafetySettings } from "./AgentSafetySettings";

beforeEach(() => { vi.clearAllMocks(); api.set.mockResolvedValue(undefined); api.rules.mockResolvedValue([]); api.deleteRule.mockResolvedValue(undefined); });
afterEach(cleanup);

it("allows YOLO until the daemon setting turns it off, and writes the setting back", async () => {
  api.all.mockResolvedValue({});
  render(<AgentSafetySettings />);
  const toggle = await screen.findByRole("switch", { name: "Allow Yolo mode" });
  await waitFor(() => expect(toggle).toBeEnabled());
  expect(toggle).toBeChecked();
  fireEvent.click(toggle);
  await waitFor(() => expect(api.set).toHaveBeenCalledWith("agents", { yoloDisabled: true }));
  await waitFor(() => expect(toggle).not.toBeChecked());
});

it("shows a stored off setting and reports a failed save without flipping the switch", async () => {
  api.all.mockResolvedValue({ agents: { yoloDisabled: true } });
  api.set.mockRejectedValue(new Error("daemon offline"));
  render(<AgentSafetySettings />);
  const toggle = await screen.findByRole("switch", { name: "Allow Yolo mode" });
  await waitFor(() => expect(toggle).toBeEnabled());
  expect(toggle).not.toBeChecked();
  fireEvent.click(toggle);
  expect(await screen.findByRole("alert")).toHaveTextContent("daemon offline");
  expect(toggle).not.toBeChecked();
});

it("names tasks with AI by default and writes only its own key, keeping the rest of the setting", async () => {
  api.all.mockResolvedValue({ agents: { yoloDisabled: true, futureKey: 7 } });
  render(<AgentSafetySettings />);
  const toggle = await screen.findByRole("switch", { name: "Name tasks with AI" });
  await waitFor(() => expect(toggle).toBeEnabled());
  expect(toggle).toBeChecked();
  expect(screen.getByText(/Uses your Claude or ChatGPT account for one short extra request per new task/)).toBeInTheDocument();
  fireEvent.click(toggle);
  await waitFor(() => expect(api.set).toHaveBeenCalledWith("agents", { yoloDisabled: true, futureKey: 7, aiTitlesDisabled: true }));
  await waitFor(() => expect(toggle).not.toBeChecked());
  expect(screen.getByRole("switch", { name: "Allow Yolo mode" })).not.toBeChecked();
});

it("keeps the AI title setting when Yolo is switched, and reports a failed save", async () => {
  api.all.mockResolvedValue({ agents: { aiTitlesDisabled: true } });
  render(<AgentSafetySettings />);
  const titles = await screen.findByRole("switch", { name: "Name tasks with AI" });
  const yolo = screen.getByRole("switch", { name: "Allow Yolo mode" });
  await waitFor(() => expect(titles).toBeEnabled());
  expect(titles).not.toBeChecked();
  fireEvent.click(yolo);
  await waitFor(() => expect(api.set).toHaveBeenCalledWith("agents", { aiTitlesDisabled: true, yoloDisabled: true }));
  api.set.mockRejectedValue(new Error("daemon offline"));
  fireEvent.click(titles);
  expect(await screen.findByRole("alert")).toHaveTextContent("daemon offline");
  expect(titles).not.toBeChecked();
});

it("writes the bot switches into the same setting without dropping other keys", async () => {
  api.all.mockResolvedValue({ agents: { yoloDisabled: false, defaultPermissionMode: "auto", futureKey: 7 } });
  render(<AgentSafetySettings />);
  const autoApprove = await screen.findByRole("switch", { name: "Bots start tasks without asking" });
  await waitFor(() => expect(autoApprove).toBeEnabled());
  expect(autoApprove).not.toBeChecked();
  expect(screen.getByRole("combobox", { name: "Default permission mode" })).toHaveTextContent("Auto");
  fireEvent.click(autoApprove);
  await waitFor(() => expect(api.set).toHaveBeenCalledWith("agents", { yoloDisabled: false, defaultPermissionMode: "auto", futureKey: 7, botAutoApprove: true }));
  fireEvent.click(screen.getByRole("switch", { name: "Bots may use Yolo" }));
  await waitFor(() => expect(api.set).toHaveBeenLastCalledWith("agents", { yoloDisabled: false, defaultPermissionMode: "auto", futureKey: 7, botAutoApprove: true, botYolo: true }));
});

it("keeps bots off Yolo and shows Manual while Yolo is turned off, even with a stored Yolo default", async () => {
  api.all.mockResolvedValue({ agents: { yoloDisabled: true, defaultPermissionMode: "yolo", botYolo: true } });
  render(<AgentSafetySettings />);
  const botYolo = await screen.findByRole("switch", { name: "Bots may use Yolo" });
  await waitFor(() => expect(screen.getByRole("switch", { name: "Allow Yolo mode" })).toBeEnabled());
  expect(botYolo).toHaveAttribute("aria-disabled", "true");
  expect(botYolo).not.toBeChecked();
  expect(screen.getByRole("combobox", { name: "Default permission mode" })).toHaveTextContent("Manual");
  expect(screen.getByText(/Needs Yolo mode allowed above/)).toBeInTheDocument();
});

it("lists remembered bot approvals and removes one through the daemon", async () => {
  api.all.mockResolvedValue({});
  api.rules.mockResolvedValue([
    { id: 3, actor: "bot:atlas", action: "agent.start", resource: "*", maxEffect: "write", decision: "allow", note: "", createdAt: 1 },
    { id: 4, actor: "bot:atlas", action: "agent.prompt", resource: "*", maxEffect: "write", decision: "allow", note: "", createdAt: 1 },
    { id: 5, actor: "user", action: "mail.send", resource: "mail/x", maxEffect: "write", decision: "allow", note: "", createdAt: 1 },
    { id: 6, actor: "bot:atlas", action: "mail.send", resource: "mail/x", maxEffect: "write", decision: "allow", note: "", createdAt: 1 },
  ]);
  render(<AgentSafetySettings />);
  const remove = await screen.findAllByRole("button", { name: "Remove" });
  expect(remove).toHaveLength(2);
  expect(screen.queryByText(/mail\.send/)).toBeNull();
  fireEvent.click(remove[0]!);
  await waitFor(() => expect(api.deleteRule).toHaveBeenCalledWith(3));
  await waitFor(() => expect(screen.getAllByRole("button", { name: "Remove" })).toHaveLength(1));
  expect(screen.getByText(/agent\.prompt/)).toBeInTheDocument();
});
