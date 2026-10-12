import { beforeEach, expect, it, vi } from "vitest";
import { domainProviders } from "./palette";
import { useUI } from "@/stores/ui";

const api = vi.hoisted(() => ({
  projects: vi.fn(), agents: vi.fn(), agent: vi.fn(), settings: vi.fn(), automations: vi.fn(),
  openProjectTasks: vi.fn(), openTask: vi.fn(), upsertAgent: vi.fn(), openSettings: vi.fn().mockResolvedValue(undefined),
}));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({
  Projects: { list: api.projects }, Agents: { list: api.agents, get: api.agent }, Automations: { list: api.automations }, Search: { query: api.settings }, Desktop: { openSettings: api.openSettings },
}));
vi.mock("@/features/projects/tasks", () => ({ openProjectTasks: api.openProjectTasks, openTask: api.openTask }));
vi.mock("@/stores/agents", () => ({ useAgents: { getState: () => ({ upsert: api.upsertAgent }) } }));
const provider = (id: string) => domainProviders.find((item) => item.id === id)!;
const signal = () => new AbortController().signal;
beforeEach(() => { vi.clearAllMocks(); useUI.setState({ view: "home" }); });


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

it.each(["skills", "mcp"])("opens %s configuration inside Settings", async (section) => {
  api.settings.mockResolvedValue({ results: [{ id: `settings/${section}/`, title: section, action: { view: "settings", section, tab: "" } }] });
  const [hit] = await provider("settings").search(section, signal());
  hit!.run();
  expect(api.openSettings).toHaveBeenCalledWith(section); // settings open in their own window
});

it("keeps ordinary configuration results in their Settings section", async () => {
  api.settings.mockResolvedValue({ results: [{ id: "settings/customization/appearance", title: "Settings: Theme and density", action: { view: "settings", section: "customization", tab: "appearance" } }] });
  const [hit] = await provider("settings").search("theme", signal());
  hit!.run();
  expect(api.openSettings).toHaveBeenCalledWith("customization");
});

it("opens the Automations view focused on a saved automation", async () => {
  api.automations.mockResolvedValue([{ definition: { id: "nightly", name: "Nightly triage", enabled: true, trigger: { type: "schedule" } } }]);
  const [hit] = await provider("automations").search("triage", signal());
  hit!.run();
  expect(useUI.getState()).toMatchObject({ view: "automations", automationFocus: "nightly" });
});
