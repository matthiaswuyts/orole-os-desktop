import { Agents, Projects, Search } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { fuzzyScore, registerProvider, type Command, type SearchProvider } from "@/lib/commands";
import { useAgents } from "@/stores/agents";
import { openProjectTasks, openTask } from "@/features/projects/tasks";
import { useUI } from "@/stores/ui";

// Metadata only: never index message bodies, file contents or terminal output.
function matches(query: string, commands: Command[]): Command[] {
  return commands.map((command) => ({ command, score: Math.max(fuzzyScore(query, command.title), fuzzyScore(query, command.category) * 0.7) }))
    .filter(({ score }) => score > 0).sort((a, b) => b.score - a.score).slice(0, 20).map(({ command }) => command);
}

export const domainProviders: SearchProvider[] = [
  {
    id: "projects", label: "Projects",
    async search(query, signal) {
      if (!inDesktop || signal.aborted) return [];
      const projects = await Projects.list();
      if (signal.aborted) return [];
      return matches(query, projects.map((project) => ({
        id: `project.${project.id}`, title: project.name, category: `${project.path} · ${project.remoteUrl}`,
        run: () => openProjectTasks(project.id),
      })));
    },
  },
  {
    id: "agents", label: "Tasks",
    async search(query, signal) {
      if (!inDesktop || signal.aborted) return [];
      const sessions = await Agents.list();
      if (signal.aborted) return [];
      return matches(query, sessions.map((session) => ({
        id: `agent.${session.id}`, title: session.title || session.kind, category: `${session.kind} · ${session.status} · ${session.project} · ${session.dir}`,
        run: async () => {
          const current = await Agents.get(session.id);
          useAgents.getState().upsert(current);
          openTask(current.id);
        },
      })));
    },
  },
  {
    id: "settings", label: "Settings",
    async search(query, signal) {
      if (!inDesktop || signal.aborted) return [];
      const response = await Search.query(query, ["settings"], 30);
      if (signal.aborted) return [];
      if (response.errors && Object.keys(response.errors).length) throw new Error(Object.values(response.errors).join("; "));
      return (response.results ?? []).map((result) => ({
        id: `setting.${result.id}`, title: result.title, category: "Settings",
        run: () => useUI.getState().openSettings(result.action.section, result.action.tab),
      }));
    },
  },
];

const unregister = domainProviders.map(registerProvider);
if (import.meta.hot) import.meta.hot.dispose(() => unregister.forEach((off) => off()));
