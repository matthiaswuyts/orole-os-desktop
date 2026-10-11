import { useId, useMemo, type Dispatch } from "react";
import { Globe } from "lucide-react";
import { Chat, MCP, Skills, type ChatToolInfo } from "@/mygo";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { splitModel, type Action, type FormState, type Problems } from "./botForm";
import { Block, MultiSelect, type MultiItem } from "./controls";
import { useLoad } from "./useLoad";

interface McpCatalog {
  items: MultiItem[];
  /** Servers whose tools could not be listed, with the daemon's reason. */
  failed: { name: string; error: string }[];
}

const failure = (cause: unknown) => (cause instanceof Error ? cause.message : String(cause)).replace(/^(bad_request|not_found|internal|denied): /, "");

/** Tools of every enabled MCP server, as "<serverId>/<tool>" values (the daemon's allow-list format). */
async function loadMcpCatalog(): Promise<McpCatalog> {
  const servers = ((await MCP.list()) ?? []).filter((s) => s.enabled);
  const settled = await Promise.allSettled(servers.map((s) => MCP.tools(s.id)));
  const catalog: McpCatalog = { items: [], failed: [] };
  settled.forEach((result, i) => {
    const server = servers[i]!;
    if (result.status === "rejected") { catalog.failed.push({ name: server.name, error: failure(result.reason) }); return; }
    for (const tool of result.value ?? []) {
      catalog.items.push({
        value: `${server.id}/${tool.name}`, label: tool.title || tool.name,
        detail: [server.name, tool.description].filter(Boolean).join(" · "),
        badge: tool.effect === "write" || tool.effect === "destructive" ? tool.effect : undefined,
      });
    }
  });
  return catalog;
}

function BuiltinTool({ info, form, dispatch }: { info: ChatToolInfo; form: FormState; dispatch: Dispatch<Action> }) {
  const heading = useId();
  const enabled = form.tools.includes(info.id);
  return (
    <li className="flex min-w-0 flex-col gap-2 py-2">
      <div className="flex items-center justify-between gap-3">
        <span className="flex min-w-0 items-center gap-1.5">
          <span id={heading} className="truncate text-sm">{info.name}</span>
          {info.egress && <Globe className="size-3 shrink-0 text-muted-foreground" aria-label={`Sends data off this computer: ${info.egress}`} />}
        </span>
        <Switch
          checked={enabled} disabled={!info.available && !enabled} aria-labelledby={heading}
          onCheckedChange={(on) => dispatch({ type: "tool", id: info.id, enabled: on })}
        />
      </div>
      {!info.available && <p role="status" className="text-muted-foreground text-xs">{info.reason || "The selected model cannot run this tool."}</p>}
    </li>
  );
}

export function ToolsTab({ form, dispatch, problems }: { form: FormState; dispatch: Dispatch<Action>; problems: Problems }) {
  const request = splitModel(form.model);
  const tools = useLoad(() => Chat.tools(request), form.model);
  const mcp = useLoad(loadMcpCatalog, "mcp");
  const skills = useLoad(() => Skills.list(), "skills");

  const skillItems = useMemo<MultiItem[]>(() => (skills.data ?? []).map((s) => ({ value: s.name, label: s.name, detail: s.description, badge: s.enabled ? undefined : "disabled" })), [skills.data]);
  const offered = new Set((tools.data ?? []).map((t) => t.id));
  const unknown = tools.data ? form.tools.filter((id) => !offered.has(id)) : [];

  return (
    <div className="flex flex-col gap-4">
      {tools.error && (
        <Alert variant="error"><AlertDescription className="flex items-center justify-between gap-2"><span className="selectable break-words">{tools.error}</span><Button size="xs" variant="outline" onClick={tools.reload}>Retry</Button></AlertDescription></Alert>
      )}
      {!tools.data && !tools.error && <p role="status" className="text-muted-foreground text-xs">Loading…</p>}
      {tools.data && tools.data.length > 0 && <ul aria-label="Built-in tools" className="flex flex-col divide-y">{tools.data.map((info) => <BuiltinTool key={info.id} info={info} form={form} dispatch={dispatch} />)}</ul>}
      {unknown.map((id) => (
        <div key={id} className="flex items-center justify-between gap-2 text-xs">
          <span className="min-w-0 break-words text-muted-foreground">{id} is not offered here.</span>
          <Button size="xs" variant="outline" onClick={() => dispatch({ type: "tool", id, enabled: false })}>Remove {id}</Button>
        </div>
      ))}

      <Block label="MCP tools" error={problems.mcpTools}>
        {(id, describedBy) => (
          <MultiSelect
            id={id} label="MCP tools" items={mcp.data?.items ?? []} value={form.mcpTools} onChange={(mcpTools) => dispatch({ type: "patch", patch: { mcpTools } })}
            placeholder="Search…" emptyText={mcp.loading ? "Loading…" : "No MCP tool matches."} invalid={!!problems.mcpTools} describedBy={describedBy}
            missingLabel="not found"
          />
        )}
      </Block>
      {mcp.error && <p role="alert" className="selectable break-words text-destructive-foreground text-xs">{mcp.error}</p>}
      {mcp.data?.failed.map((f) => <p key={f.name} role="status" className="selectable break-words text-warning-foreground text-xs">{f.name}: {f.error}</p>)}

      <Block label="Skills" error={problems.skills}>
        {(id, describedBy) => (
          <MultiSelect
            id={id} label="Skills" items={skillItems} value={form.skills} onChange={(next) => dispatch({ type: "patch", patch: { skills: next } })}
            placeholder="Search…" emptyText={skills.loading ? "Loading…" : "No skill matches."} invalid={!!problems.skills} describedBy={describedBy}
          />
        )}
      </Block>
      {skills.error && <p role="alert" className="selectable break-words text-destructive-foreground text-xs">{skills.error}</p>}
    </div>
  );
}
