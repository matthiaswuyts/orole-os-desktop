import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { HarnessOption, LaunchOptions } from "@/mygo";

const api = vi.hoisted(() => ({ all: vi.fn(), set: vi.fn(), rules: vi.fn(), deleteRule: vi.fn(), bots: vi.fn(), options: vi.fn() }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({
  Settings: { all: api.all, set: api.set }, Policy: { rules: api.rules, deleteRule: api.deleteRule }, Chat: { bots: api.bots }, Agents: { options: api.options },
}));
import { useDaemon } from "@/stores/daemon";
import { useHarnessOptionsCache } from "@/stores/harnessOptions";
import { BotCodeTasksSettings, BotToolsSettings, TaskNamingSettings, TaskPermissionsSettings, useAgentsSetting } from "./AgentSafetySettings";

/** Every page that edits the shared "agents" setting at once, so a save from one is checked against the others. */
const AgentSafetySettings = () => <><TaskPermissionsSettings /><TaskNamingSettings /><BotToolsSettings /><BotCodeTasksSettings /></>;

const codex: HarnessOption = {
  id: "codex", name: "Codex", available: true, executable: "codex", version: "1", reasoning: ["low", "high"], customModel: true, modes: ["structured"], structured: true,
  interactive: false, unavailableReason: "", permissionModes: [],
  models: [{ id: "model-a", name: "Model A", reasoning: ["low", "high"], default: true }, { id: "model-b", name: "Model B", reasoning: [], default: false }],
};
const options: LaunchOptions = { machine: "local", harnesses: [codex, { ...codex, id: "custom", name: "Custom command" }], projects: [], directories: [], worktree: true, unavailableReason: "" };

beforeEach(() => {
  vi.clearAllMocks();
  api.set.mockResolvedValue(undefined); api.rules.mockResolvedValue([]); api.deleteRule.mockResolvedValue(undefined); api.bots.mockResolvedValue([]); api.options.mockResolvedValue(options);
  useDaemon.setState({ link: "connected", connectEpoch: 0 });
  useHarnessOptionsCache.setState({ machines: {} });
  useAgentsSetting.setState({ setting: null, error: null });
});
afterEach(cleanup);

/** Opens a Base UI select by its accessible name and picks an option (the popup ignores clicks while it animates in). */
async function pick(combobox: string | RegExp, option: string | RegExp) {
  const trigger = screen.getByRole("combobox", { name: combobox });
  await waitFor(() => expect(trigger).not.toHaveAttribute("data-disabled"));
  fireEvent.pointerDown(trigger, { button: 0, pointerType: "mouse" });
  fireEvent.mouseDown(trigger, { button: 0 });
  const item = await screen.findByRole("option", { name: option });
  const { promise: settled, resolve } = Promise.withResolvers<void>();
  setTimeout(resolve, 400);
  await settled;
  fireEvent.pointerUp(item, { button: 0, pointerType: "mouse" });
  fireEvent.mouseUp(item, { button: 0 });
  fireEvent.click(item);
}

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
  expect((await screen.findAllByRole("alert"))[0]).toHaveTextContent("daemon offline");
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
  expect((await screen.findAllByRole("alert"))[0]).toHaveTextContent("daemon offline");
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

it("lists every remembered bot rule by bot name and removes one through the daemon", async () => {
  api.all.mockResolvedValue({});
  api.bots.mockResolvedValue([{ id: "atlas", name: "Atlas" }]);
  api.rules.mockResolvedValue([
    { id: 3, actor: "bot:atlas", action: "agent.start", resource: "*", maxEffect: "write", decision: "allow", note: "", createdAt: 1 },
    { id: 4, actor: "bot:atlas", action: "agent.prompt", resource: "*", maxEffect: "write", decision: "allow", note: "", createdAt: 1 },
    { id: 5, actor: "user", action: "mail.send", resource: "mail/x", maxEffect: "write", decision: "allow", note: "", createdAt: 1 },
    { id: 6, actor: "bot:atlas", action: "mcp.tool.call", resource: "mcp/linear/create_issue", maxEffect: "write", decision: "allow", note: "", createdAt: 1 },
  ]);
  render(<AgentSafetySettings />);
  const remove = await screen.findAllByRole("button", { name: "Remove" });
  expect(remove).toHaveLength(3);
  expect(screen.queryByText(/mail\/x/)).toBeNull();
  expect(await screen.findByText(/mcp\/linear\/create_issue/)).toBeInTheDocument();
  expect(screen.getAllByText("Atlas")).toHaveLength(3);
  fireEvent.click(remove[0]!);
  await waitFor(() => expect(api.deleteRule).toHaveBeenCalledWith(3));
  await waitFor(() => expect(screen.getAllByRole("button", { name: "Remove" })).toHaveLength(2));
  expect(screen.getByText(/agent\.prompt/)).toBeInTheDocument();
});

it("writes the bot tool level into the whole setting", async () => {
  api.all.mockResolvedValue({ agents: { botAutoApprove: true, futureKey: 7 } });
  render(<AgentSafetySettings />);
  await pick("Without asking, bots may", /^Everything without asking/);
  await waitFor(() => expect(api.set).toHaveBeenCalledWith("agents", { botAutoApprove: true, futureKey: 7, botToolAccess: "all" }));
});

it("sets the code-task agent, then a model of that agent, keeping every other key", async () => {
  api.all.mockResolvedValue({ agents: { defaultPermissionMode: "auto", futureKey: 7, botTasks: { permissionMode: "manual" } } });
  render(<AgentSafetySettings />);
  const model = await screen.findByRole("combobox", { name: "Model" });
  expect(model).toHaveAttribute("data-disabled");
  await pick("Agent", "Codex");
  await waitFor(() => expect(api.set).toHaveBeenCalledWith("agents", { defaultPermissionMode: "auto", futureKey: 7, botTasks: { permissionMode: "manual", harness: "codex", model: "auto", reasoning: "auto" } }));
  await pick("Model", "Model B");
  await waitFor(() => expect(api.set).toHaveBeenLastCalledWith("agents", expect.objectContaining({ futureKey: 7, botTasks: { permissionMode: "manual", harness: "codex", model: "model-b", reasoning: "auto" } })));
  expect(screen.queryByRole("option", { name: "Custom command" })).toBeNull();
});

it("names the inherited default mode and keeps Yolo unavailable to bots until both Yolo switches allow it", async () => {
  api.all.mockResolvedValue({ agents: { defaultPermissionMode: "auto" } });
  render(<AgentSafetySettings />);
  const mode = await screen.findByRole("combobox", { name: "Bot permission mode" });
  await waitFor(() => expect(mode).toHaveTextContent("Same as my default (Auto)"));
  fireEvent.pointerDown(mode, { button: 0, pointerType: "mouse" });
  fireEvent.mouseDown(mode, { button: 0 });
  expect(await screen.findByRole("option", { name: /Yolo.*Bots may use Yolo/ })).toHaveAttribute("aria-disabled", "true");
});
