import { useCallback } from "react";
import { Workflow } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Widget } from "@/components/widgets/Widget";
import type { WidgetProps } from "@/components/widgets/registry";
import { inDesktop } from "@/lib/backend";
import { useDaemon } from "@/stores/daemon";
import { useUI } from "@/stores/ui";
import { api } from "./api";
import { duration } from "./flow";
import { RunStatus } from "./RunsPanel";
import { useAutomationResource } from "./useAutomationResource";

export function AutomationResultsWidget({ onRemove }: WidgetProps) {
  const load = useCallback(() => api.runs(""), []);
  const resource = useAutomationResource(load);
  const offline = useDaemon((s) => s.link === "offline");
  const setView = useUI((s) => s.setView);
  const status = !inDesktop || offline ? "offline" : resource.error ? "error" : resource.loading ? "loading" : "ready";
  const message = !inDesktop ? "Open Orole OS desktop to see automation results." : offline ? "Daemon offline. Reconnect to load run results." : resource.error ?? undefined;
  return <Widget title="Automation results" icon={<Workflow />} status={status} message={message} onRemove={onRemove} action={resource.error ? { label: "Retry", onClick: () => void resource.refresh() } : { label: "Open automations", onClick: () => setView("automations") }}>
    {!resource.data?.length && <Empty><EmptyHeader><EmptyTitle>No runs yet</EmptyTitle><EmptyDescription>Run an automation to see its results here.</EmptyDescription></EmptyHeader></Empty>}
    <ul className="flex flex-col divide-y">{resource.data?.slice(0, 6).map((run) => <li key={run.id} className="flex min-w-0 flex-col gap-1 py-2"><div className="flex items-center gap-2"><span className="min-w-0 flex-1 truncate font-medium">{run.name}</span><RunStatus status={run.status} /></div><div className="flex flex-wrap justify-between gap-1 text-xs text-muted-foreground tabular-nums"><time dateTime={new Date(run.createdAt).toISOString()}>{new Date(run.createdAt).toLocaleString()}</time><span>{duration(run.startedAt, run.finishedAt)}</span></div></li>)}</ul>
    <Button size="xs" variant="outline" className="mt-2" onClick={() => setView("automations")}>Open automations</Button>
  </Widget>;
}
