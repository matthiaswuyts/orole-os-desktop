import { beforeEach, expect, it, vi } from "vitest";
import { domainProviders } from "./palette";
import { useUI } from "@/stores/ui";

const api = vi.hoisted(() => ({
  mailStatus: vi.fn(), mailSearch: vi.fn(), driveStatus: vi.fn(), driveSearch: vi.fn(),
  projects: vi.fn(), agents: vi.fn(), agent: vi.fn(), terminals: vi.fn(), settings: vi.fn(),
  selectProject: vi.fn(), upsertAgent: vi.fn(), selectAgent: vi.fn(),
}));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({
  Mail: { status: api.mailStatus, search: api.mailSearch }, Drive: { status: api.driveStatus, search: api.driveSearch },
  Projects: { list: api.projects }, Agents: { list: api.agents, get: api.agent }, Terminals: { list: api.terminals }, Search: { query: api.settings },
}));
vi.mock("@/stores/projects", () => ({ useProjects: { getState: () => ({ select: api.selectProject }) } }));
vi.mock("@/stores/agents", () => ({ useAgents: { getState: () => ({ setOpenId: api.selectAgent, upsert: api.upsertAgent }) } }));
const provider = (id: string) => domainProviders.find((item) => item.id === id)!;
const signal = () => new AbortController().signal;
beforeEach(() => { vi.clearAllMocks(); useUI.setState({ view: "home", pendingMailThread: null, pendingDriveFile: null, pendingTerminal: null }); });

it("never invokes content search for disconnected Mail or Drive", async () => {
  api.mailStatus.mockResolvedValue({ connected: false });
  api.driveStatus.mockResolvedValue({ connected: false });
  expect(provider("mail").explicitOnly).toBe(true);
  expect(provider("drive").explicitOnly).toBe(true);
  await expect(provider("mail").search("hello", signal())).rejects.toThrow("disconnected");
  await expect(provider("drive").search("hello", signal())).rejects.toThrow("disconnected");
  expect(api.mailSearch).not.toHaveBeenCalled(); expect(api.driveSearch).not.toHaveBeenCalled();
});

it("does not start a sensitive search after status discovery was cancelled", async () => {
  const controller = new AbortController();
  api.mailStatus.mockImplementation(async () => { controller.abort(); return { connected: true, capabilities: { canSearch: true } }; });
  expect(await provider("mail").search("hello", controller.signal)).toEqual([]);
  expect(api.mailSearch).not.toHaveBeenCalled();
});

