import { useId, useMemo, type Dispatch } from "react";
import { Globe } from "lucide-react";
import { Chat, MCP, Skills, type ChatToolInfo } from "@/mygo";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { useChat } from "@/stores/chat";
import { LIMITS, TOOL_IDS, emptyToolSettings, splitModel, type Action, type FormState, type Problems, type ToolSettings } from "./botForm";
import { Block, DomainList, MultiSelect, Section, type MultiItem } from "./controls";
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

function BuiltinTool({ info, form, dispatch, problems }: { info: ChatToolInfo; form: FormState; dispatch: Dispatch<Action>; problems: Problems }) {
  const heading = useId();
  const detail = useId();
  const settings = form.tools[info.id];
  const enabled = settings !== undefined;
  const patch = (value: Partial<ToolSettings>) => dispatch({ type: "toolPatch", id: info.id, patch: value });
  const isSearch = info.id === TOOL_IDS.webSearch;
  const isFetch = info.id === TOOL_IDS.webFetch;
  const key = (field: string) => `tool:${info.id}:${field}`;
  return (
    <li className="flex min-w-0 flex-col gap-2 rounded-md border bg-background/40 p-2.5">
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 flex-col gap-0.5">
          <span id={heading} className="font-medium text-sm">{info.name}</span>
          <span id={detail} className="text-muted-foreground text-xs">{info.description}</span>
          {info.egress && <span className="mt-0.5 flex items-start gap-1 text-muted-foreground text-xs"><Globe className="mt-0.5 size-3 shrink-0" aria-hidden="true" /><span>Leaves this computer: {info.egress}</span></span>}
          {!info.available && <span role="status" className="text-warning-foreground text-xs">{enabled ? "Enabled, but not available now: " : "Not available: "}{info.reason || "The selected model cannot run this tool."}</span>}
        </div>
        <Switch
          checked={enabled} disabled={!info.available && !enabled}
          aria-labelledby={heading} aria-describedby={detail}
          onCheckedChange={(on) => dispatch({ type: "tool", id: info.id, settings: on ? emptyToolSettings() : null })}
        />
      </div>
      {enabled && settings && (isSearch || isFetch) && (
        <div className="flex flex-col gap-3 border-t pt-2.5">
          {isSearch && (
            <Block label="Max results" error={problems[key("maxResults")]} hint={`Sources per search, ${LIMITS.maxResults.min}–${LIMITS.maxResults.max}. Empty uses the provider default.`}>
              {(id, describedBy) => (
                <Input nativeInput id={id} type="number" inputMode="numeric" className="w-28" min={LIMITS.maxResults.min} max={LIMITS.maxResults.max} step={1} value={settings.maxResults} aria-invalid={!!problems[key("maxResults")] || undefined} aria-describedby={describedBy} onChange={(e) => patch({ maxResults: e.target.value })} />
              )}
            </Block>
          )}
          {isFetch && (
            <Block label="Max page size (tokens)" error={problems[key("maxContentTokens")]} hint={`The most text of one page the bot reads, up to ${LIMITS.maxContentTokens.max.toLocaleString("en-US")}. Empty uses the provider default.`}>
              {(id, describedBy) => (
                <Input nativeInput id={id} type="number" inputMode="numeric" className="w-36" min={LIMITS.maxContentTokens.min} max={LIMITS.maxContentTokens.max} step={1} value={settings.maxContentTokens} aria-invalid={!!problems[key("maxContentTokens")] || undefined} aria-describedby={describedBy} onChange={(e) => patch({ maxContentTokens: e.target.value })} />
              )}
            </Block>
          )}
          <DomainList label="Allowed domains" value={settings.allowed} max={LIMITS.domains} hint="Only these sites are used. Empty allows any site." onChange={(allowed) => patch({ allowed })} />
          {problems[key("allowed")] && <p role="alert" className="text-destructive-foreground text-xs">{problems[key("allowed")]}</p>}
          <DomainList label="Blocked domains" value={settings.blocked} max={LIMITS.domains} hint="These sites are never used." onChange={(blocked) => patch({ blocked })} />
          {problems[key("blocked")] && <p role="alert" className="text-destructive-foreground text-xs">{problems[key("blocked")]}</p>}
        </div>
      )}
    </li>
  );
}

export function ToolsTab({ form, dispatch, problems, botId }: { form: FormState; dispatch: Dispatch<Action>; problems: Problems; botId: string | null }) {
  const request = splitModel(form.model);
  const tools = useLoad(() => Chat.tools(request), form.model);
  const mcp = useLoad(loadMcpCatalog, "mcp");
  const skills = useLoad(() => Skills.list(), "skills");
  const bots = useChat((s) => s.bots);

  const skillItems = useMemo<MultiItem[]>(() => (skills.data ?? []).map((s) => ({ value: s.name, label: s.name, detail: s.description, badge: s.enabled ? undefined : "disabled" })), [skills.data]);
  const botItems = useMemo<MultiItem[]>(() => bots.filter((b) => b.id !== botId).map((b) => ({ value: b.id, label: b.name, detail: b.description })), [bots, botId]);
  const offered = new Set((tools.data ?? []).map((t) => t.id));
  const unknown = tools.data ? Object.keys(form.tools).filter((id) => !offered.has(id)) : [];

  return (
    <div className="flex flex-col gap-3">
      <Section title="Built-in tools" description="What the bot may do on its own while answering. Tools that need the internet send data off this computer, as noted on each one.">
        {tools.error && (
          <Alert variant="error"><AlertDescription className="flex items-center justify-between gap-2"><span className="selectable break-words">Could not load the tools: {tools.error}</span><Button size="xs" variant="outline" onClick={tools.reload}>Try again</Button></AlertDescription></Alert>
        )}
        {!tools.data && !tools.error && <p role="status" className="text-muted-foreground text-xs">Loading tools…</p>}
        {tools.data && tools.data.length === 0 && <p className="text-muted-foreground text-xs">The daemon offers no tools for this model.</p>}
        {tools.data && tools.data.length > 0 && <ul aria-label="Built-in tools" className="flex flex-col gap-2">{tools.data.map((info) => <BuiltinTool key={info.id} info={info} form={form} dispatch={dispatch} problems={problems} />)}</ul>}
        {unknown.map((id) => (
          <div key={id} className="flex items-center justify-between gap-2 rounded-md border border-warning/32 bg-warning/4 p-2 text-xs">
            <span className="min-w-0 break-words"><span className="font-medium">{id}</span> is enabled but this daemon does not offer it, so it is ignored.</span>
            <Button size="xs" variant="outline" onClick={() => dispatch({ type: "tool", id, settings: null })}>Remove {id}</Button>
          </div>
        ))}
      </Section>

      <Section title="MCP tools" description="Tools from your connected MCP servers. Each call still goes through your approval rules before it runs.">
        <Block label="Allowed MCP tools" error={problems.mcpTools}>
          {(id, describedBy) => (
            <MultiSelect
              id={id} label="MCP tools" items={mcp.data?.items ?? []} value={form.mcpTools} onChange={(mcpTools) => dispatch({ type: "patch", patch: { mcpTools } })}
              placeholder="Search MCP tools…" emptyText={mcp.loading ? "Loading tools…" : "No MCP tool matches."} invalid={!!problems.mcpTools} describedBy={describedBy}
              missingLabel="server or tool not found"
            />
          )}
        </Block>
        {mcp.error && <p role="alert" className="selectable break-words text-destructive-foreground text-xs">Could not list MCP servers: {mcp.error}</p>}
        {mcp.data?.failed.map((f) => <p key={f.name} role="status" className="selectable break-words text-warning-foreground text-xs">Could not list the tools of {f.name}: {f.error}</p>)}
        {mcp.data && mcp.data.items.length === 0 && mcp.data.failed.length === 0 && <p className="text-muted-foreground text-xs">No enabled MCP server offers tools. Add one in Settings.</p>}
      </Section>

      <Section title="Skills" description="Instructions from your skills library the bot may load when they fit the task.">
        <Block label="Skills" error={problems.skills}>
          {(id, describedBy) => (
            <MultiSelect
              id={id} label="Skills" items={skillItems} value={form.skills} onChange={(next) => dispatch({ type: "patch", patch: { skills: next } })}
              placeholder="Search skills…" emptyText={skills.loading ? "Loading skills…" : "No skill matches."} invalid={!!problems.skills} describedBy={describedBy}
            />
          )}
        </Block>
        {skills.error && <p role="alert" className="selectable break-words text-destructive-foreground text-xs">Could not load skills: {skills.error}</p>}
        {skills.data && skills.data.length === 0 && <p className="text-muted-foreground text-xs">Your skills library is empty.</p>}
      </Section>

      <Section title="Ask other bots" description="Bots this one may put a question to. Their answers come back as untrusted data: this bot treats them as information, never as instructions.">
        <Block label="Bots it may ask" error={problems.delegateTo}>
          {(id, describedBy) => (
            <MultiSelect
              id={id} label="Bots it may ask" items={botItems} value={form.delegateTo} onChange={(delegateTo) => dispatch({ type: "patch", patch: { delegateTo } })}
              placeholder="Search bots…" emptyText="No other bot matches." invalid={!!problems.delegateTo} describedBy={describedBy} missingLabel="bot not found"
            />
          )}
        </Block>
        {botItems.length === 0 && <p className="text-muted-foreground text-xs">You have no other bots yet.</p>}
        <p className="text-muted-foreground text-xs">Asking other bots is not available in the preview.</p>
        <Badge variant="outline" size="sm" className="self-start">Delegated answers are untrusted</Badge>
      </Section>
    </div>
  );
}
