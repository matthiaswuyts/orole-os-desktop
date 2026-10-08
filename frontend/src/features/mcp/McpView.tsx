import { inDesktop } from "@/lib/backend";
import { NeedsDesktop } from "@/features/shared/States";
import { MCPSettings } from "./MCPSettings";

/** Top-level MCP servers view: one list for every harness, served through the Orole gateway. */
export function McpView() {
  if (!inDesktop) return <NeedsDesktop what="MCP servers" />;
  return <div className="flex h-full min-h-0 flex-col" data-testid="mcp-view">
    <header className="shrink-0 border-b px-4 py-2.5">
      <h1 className="text-sm font-medium">MCP servers</h1>
      <p className="text-xs text-muted-foreground">Servers added here are available to every harness through the Orole gateway.</p>
    </header>
    <div className="min-h-0 flex-1 overflow-auto p-4"><MCPSettings /></div>
  </div>;
}
