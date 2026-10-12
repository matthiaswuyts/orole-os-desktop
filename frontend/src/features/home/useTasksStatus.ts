import type { WidgetStatus } from "@/components/widgets/Widget";
import { useAgents } from "@/stores/agents";
import { useDaemon } from "@/stores/daemon";

/** The shared Widget state for Home widgets built on the task list. */
export function useTasksStatus(): { status: WidgetStatus; message?: string } {
  const available = useAgents((s) => s.available), loaded = useAgents((s) => s.loaded), error = useAgents((s) => s.error), offline = useAgents((s) => s.offline);
  const link = useDaemon((s) => s.link);
  if (!available) return { status: "offline", message: "Open Orole OS desktop to see your tasks." };
  if (link === "offline" || offline) return { status: "offline", message: "Daemon offline. Reconnect to load your tasks." };
  if (error) return { status: "error", message: error };
  return loaded ? { status: "ready" } : { status: "loading" };
}
