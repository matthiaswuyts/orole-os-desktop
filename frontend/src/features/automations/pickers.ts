import { useEffect, useMemo, useState } from "react";
import { MCP, type ServerState } from "@/mygo";
import { useChat } from "@/stores/chat";
import { useProjects } from "@/stores/projects";
import { errorText } from "./api";
import type { Names } from "./flow";

/** Bots and projects for the pickers and block summaries; loads the stores when nothing else has. */
export function useNames() {
  const bots = useChat((s) => s.bots);
  const projects = useProjects((s) => s.projects);
  useEffect(() => {
    // Read at effect time: a sibling's load in the same commit already marked the store loading.
    const chat = useChat.getState();
    if (chat.available && !chat.ready && !chat.loading) void chat.load();
    const registry = useProjects.getState();
    if (!registry.loaded && !registry.loading) void registry.load();
  }, []);
  const names = useMemo<Names>(() => ({
    bot: (id) => bots.find((b) => b.id === id)?.name,
    project: (id) => projects.find((p) => p.id === id)?.name,
  }), [bots, projects]);
  return { names, bots, projects };
}

/** Connected MCP integrations, fetched once while `enabled`. */
export function useMcpServers(enabled: boolean) {
  const [servers, setServers] = useState<ServerState[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (!enabled || servers) return;
    let active = true;
    MCP.list().then((list) => { if (active) setServers(list ?? []); }, (e) => { if (active) setError(errorText(e)); });
    return () => { active = false; };
  }, [enabled, servers]);
  return { servers, error };
}
