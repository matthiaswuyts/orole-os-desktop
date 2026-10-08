import { useEffect, useRef, useState } from "react";
import { Puzzle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Widget } from "@/components/widgets/Widget";
import { registerWidgets, type WidgetProps } from "@/components/widgets/registry";
import { registerCommands } from "@/lib/commands";
import { isPluginActive, usePlugins } from "@/stores/plugins";
import { PluginActionView } from "./PluginActionView";
import { pluginWidgetType } from "./manifest";

interface Selection { id: string; digest: string; actionId: string; title: string }

/** One stable component for every declarative widget; identity is never created during render. */
function PluginWidget({ widgetType, onRemove }: WidgetProps) {
  const { records, staleIds, ready } = usePlugins();
  const record = records.find((item) => item.manifest.widgets.some((widget) => pluginWidgetType(item.manifest.id, widget.id) === widgetType));
  const widget = record?.manifest.widgets.find((item) => pluginWidgetType(record.manifest.id, item.id) === widgetType);
  const action = record?.manifest.actions.find((item) => item.id === widget?.actionId);
  if (!record || !widget || !action || !ready || staleIds.includes(record.manifest.id) || !isPluginActive(record)) {
    return <Widget title="Unavailable plugin widget" status="unconfigured" message="The plugin needs review, is offline, or has been removed. Your saved layout is unchanged." onRemove={onRemove} action={{ label: "Remove widget", onClick: onRemove }} />;
  }
  return <Widget title={widget.title} icon={<Puzzle />} onRemove={onRemove}>
    <p className="mb-3 break-words text-xs text-muted-foreground">{record.manifest.name}{widget.description ? ` · ${widget.description}` : ""}</p>
    <PluginActionView key={`${record.digest}:${action.id}`} record={record} action={action} />
  </Widget>;
}

/** Mount exactly once in App beside PolicyApprovals, not inside a route or a modal. */
export function PluginContributions() {
  const { records, staleIds, ready, startSync } = usePlugins();
  const [selection, setSelection] = useState<Selection | null>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  const previousFocus = useRef<HTMLElement | null>(null);
  useEffect(() => startSync(), [startSync]);
  useEffect(() => {
    if (!ready) return;
    const active = records.filter((record) => isPluginActive(record) && !staleIds.includes(record.manifest.id));
    const offCommands = registerCommands(active.flatMap((record) => record.manifest.commands.map((command) => ({
      id: `plugin:${record.manifest.id}:command:${command.id}`,
      title: command.title,
      category: `Plugin · ${record.manifest.name}`,
      keywords: [record.manifest.id, command.description],
      icon: <Puzzle aria-hidden="true" />,
      run: () => {
        previousFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        setSelection({ id: record.manifest.id, digest: record.digest, actionId: command.actionId, title: command.title });
      },
    }))));
    const offWidgets = registerWidgets(active.flatMap((record) => record.manifest.widgets.map((widget) => ({
      type: pluginWidgetType(record.manifest.id, widget.id),
      title: widget.title,
      description: `${record.manifest.name} · ${widget.description || "Run an MCP action on demand."}`,
      w: 6, h: 8,
      component: PluginWidget,
    }))));
    return () => { offCommands(); offWidgets(); };
  }, [records, staleIds, ready]);
  useEffect(() => { if (selection) heading.current?.focus(); }, [selection]);
  if (!selection) return null;
  const record = records.find((item) => item.manifest.id === selection.id && item.digest === selection.digest);
  const action = record?.manifest.actions.find((item) => item.id === selection.actionId);
  const available = record && action && ready && !staleIds.includes(selection.id) && isPluginActive(record);
  return <section aria-labelledby="plugin-operation-title" className="fixed bottom-4 left-4 flex max-h-[70vh] w-[min(32rem,calc(100vw-2rem))] flex-col overflow-hidden rounded-lg border bg-card shadow-lg">
    <header className="flex shrink-0 items-center gap-3 border-b p-3">
      <h2 id="plugin-operation-title" ref={heading} tabIndex={-1} className="min-w-0 flex-1 break-words font-medium outline-none">{selection.title}</h2>
      <Button size="sm" variant="ghost" onClick={() => { setSelection(null); if (previousFocus.current?.isConnected) previousFocus.current.focus(); }}>Close</Button>
    </header>
    <div className="flex min-h-0 flex-col gap-3 overflow-y-auto p-3">
      <p className="selectable break-all font-mono text-xs text-muted-foreground">{selection.id}</p>
      {record && action && available ? <PluginActionView key={`${selection.id}:${selection.digest}:${selection.actionId}`} record={record} action={action} /> : <p role="status" className="text-muted-foreground">This command is unavailable or its manifest changed. Review the plugin in Settings, then open its current command.</p>}
      <p className="text-xs text-muted-foreground">Closing this panel does not cancel a running action. Revoke the plugin in Settings to cancel its active calls.</p>
    </div>
  </section>;
}
