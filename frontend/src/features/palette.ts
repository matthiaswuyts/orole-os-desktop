import { Agents, Drive, Mail, Projects, Search, Terminals } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { fuzzyScore, registerProvider, type Command, type SearchProvider } from "@/lib/commands";
import { useAgents } from "@/stores/agents";
import { useProjects } from "@/stores/projects";
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
        run: () => { useProjects.getState().select(project.id); useUI.getState().setView("projects"); },
      })));
    },
  },
  {
    id: "agents", label: "Agents",
    async search(query, signal) {
      if (!inDesktop || signal.aborted) return [];
      const sessions = await Agents.list();
      if (signal.aborted) return [];
      return matches(query, sessions.map((session) => ({
        id: `agent.${session.id}`, title: session.title || session.kind, category: `${session.kind} · ${session.status} · ${session.project} · ${session.dir}`,
        run: async () => {
          const current = await Agents.get(session.id);
          useAgents.getState().upsert(current);
          useAgents.getState().setOpenId(current.id);
          useUI.getState().setView("agents");
        },
      })));
    },
  },
  {
    id: "terminals", label: "Terminals",
    async search(query, signal) {
      if (!inDesktop || signal.aborted) return [];
      const terminals = await Terminals.list();
      if (signal.aborted) return [];
      return matches(query, terminals.map((terminal) => ({
        id: `terminal.${terminal.id}`, title: terminal.title, category: `${terminal.shell} · ${terminal.cwd} · ${terminal.state}`,
        run: () => useUI.getState().openTerminal(terminal.id),
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
        run: () => useUI.getState().openSettings(result.action.section),
      }));
    },
  },
  {
    id: "mail", label: "Mail", explicitOnly: true,
    async search(query, signal) {
      if (!inDesktop || signal.aborted) return [];
      const status = await Mail.status();
      if (signal.aborted) return [];
      if (!status.connected) throw new Error(status.error || "Mail is disconnected. Configure it in Mail.");
      if (!status.capabilities.canSearch) throw new Error("The Mail server does not support search.");
      const page = await Mail.search({ folderId: "", query, pageToken: "", limit: 20 });
      if (signal.aborted) return [];
      return (page.messages ?? []).map((message) => ({
        id: `mail.${message.id}`, title: message.subject || "(No subject)", category: `${message.from} · ${message.date}`,
        run: () => useUI.getState().openMailThread(message.threadId || message.id),
      }));
    },
  },
  {
    id: "drive", label: "Drive", explicitOnly: true,
    async search(query, signal) {
      if (!inDesktop || signal.aborted) return [];
      const status = await Drive.status();
      if (signal.aborted) return [];
      if (!status.connected) throw new Error(status.error || "Drive is disconnected. Configure it in Drive.");
      if (!status.capabilities.canSearch) throw new Error("The Drive server does not support search.");
      const page = await Drive.search({ parentId: "", query, pageToken: "", limit: 20 });
      if (signal.aborted) return [];
      return (page.files ?? []).map((file) => ({
        id: `drive.${file.id}`, title: file.name, category: file.isFolder ? "Drive folder" : `Drive · ${file.mimeType}`,
        run: () => useUI.getState().openDriveFile({ id: file.id, name: file.name, isFolder: file.isFolder }),
      }));
    },
  },
];

const unregister = domainProviders.map(registerProvider);
if (import.meta.hot) import.meta.hot.dispose(() => unregister.forEach((off) => off()));
