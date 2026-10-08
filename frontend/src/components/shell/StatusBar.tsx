import { useDaemon } from "@/stores/daemon";
import { useWorkspaces } from "@/stores/workspaces";
import { cn } from "@/lib/utils";

const Item = ({ label, value }: { label: string; value: string }) => (
  <span className="flex gap-1"><span className="text-muted-foreground/70">{label}</span><span>{value}</span></span>
);

export function StatusBar() {
  const { link, health } = useDaemon();
  const ws = useWorkspaces((s) => s.list.find((w) => w.id === s.activeId));
  return (
    <footer className="flex h-6 shrink-0 items-center gap-4 border-t bg-sidebar px-3 font-mono text-muted-foreground text-xs">
      <span className="flex items-center gap-1.5"><span className={cn(link === "connected" ? "text-success-foreground" : link === "offline" ? "text-destructive-foreground" : "")}>●</span>
        daemon {link === "connected" ? `v${health?.version}` : link}</span>
      <Item label="agents" value="0" />
      <Item label="tasks" value="0" />
      <Item label="mcp" value="0 servers" />
      <span className="flex-1" />
      {ws && <Item label="workspace" value={ws.name} />}
      {health && <Item label="mem" value={`${health.memAllocMB.toFixed(0)} MB`} />}
    </footer>
  );
}
