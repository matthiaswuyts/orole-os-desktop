import { beforeEach, expect, it, vi } from "vitest";
import { domainProviders } from "./palette";
import { useUI } from "@/stores/ui";

const api = vi.hoisted(() => ({
  projects: vi.fn(), agents: vi.fn(), agent: vi.fn(), settings: vi.fn(),
  openProjectTasks: vi.fn(), openTask: vi.fn(), upsertAgent: vi.fn(),
}));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({
  Projects: { list: api.projects }, Agents: { list: api.agents, get: api.agent }, Search: { query: api.settings },
}));
vi.mock("@/features/projects/tasks", () => ({ openProjectTasks: api.openProjectTasks, openTask: api.openTask }));
vi.mock("@/stores/agents", () => ({ useAgents: { getState: () => ({ upsert: api.upsertAgent }) } }));
const provider = (id: string) => domainProviders.find((item) => item.id === id)!;
const signal = () => new AbortController().signal;
beforeEach(() => { vi.clearAllMocks(); useUI.setState({ view: "home" }); });

it("offers only providers for modules that exist", () => {
  expect(domainProviders.map((item) => item.id)).toEqual(["projects", "agents", "settings"]);
});

it("opens a project's tasks from a project result", async () => {
  api.projects.mockResolvedValue([{ id: "p1", name: "Alpha", path: "C:\\code\\alpha", remoteUrl: "" }]);
  const [hit] = await provider("projects").search("alpha", signal());
  hit!.run();
  expect(api.openProjectTasks).toHaveBeenCalledExactlyOnceWith("p1");
});

it("opens a task from an agent result after refreshing it", async () => {
  api.agents.mockResolvedValue([{ id: "s1", title: "Fix login", kind: "omp", status: "working", project: "C:\\code\\alpha", dir: "C:\\code\\alpha" }]);
  api.agent.mockResolvedValue({ id: "s1" });
  const [hit] = await provider("agents").search("login", signal());
  await hit!.run();
  expect(api.upsertAgent).toHaveBeenCalledWith({ id: "s1" });
  expect(api.openTask).toHaveBeenCalledExactlyOnceWith("s1");
});

it("opens MCP configuration in its top-level view", async () => {
  api.settings.mockResolvedValue({ results: [{ id: "mcp//", title: "MCP servers", action: { view: "mcp", section: "", tab: "" } }] });
  const [hit] = await provider("settings").search("mcp", signal());
  hit!.run();
  expect(useUI.getState().view).toBe("mcp");
});

it("keeps ordinary configuration results in their Settings section", async () => {
  api.settings.mockResolvedValue({ results: [{ id: "settings/customization/appearance", title: "Settings: Theme and density", action: { view: "settings", section: "customization", tab: "appearance" } }] });
  const [hit] = await provider("settings").search("theme", signal());
  hit!.run();
  expect(useUI.getState().view).toBe("settings");
});
