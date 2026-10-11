import { useEffect } from "react";
import { useDaemon } from "@/stores/daemon";
import { useAgentCounts, useAgents } from "@/stores/agents";
import { boxStatus, startBoxesPolling, useBoxes, type BoxObservation } from "@/stores/boxes";
import { useUpdates } from "@/stores/updates";
import { useUI } from "@/stores/ui";
import { cn } from "@/lib/utils";
import type { Machine } from "@/mygo";
import { PreviewCard, PreviewCardPopup, PreviewCardTrigger } from "@/components/ui/preview-card";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { Meter, MeterTrack, MeterIndicator } from "@/components/ui/meter";
import { ArchChip, OsLogo } from "./HostLogo";

const footerControl = "inline-flex h-6 shrink-0 items-center gap-1.5 rounded px-1 outline-none hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring";
const bytes = new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 });
function memoryBytes(value: number) {
  return value >= 1024 ** 3 ? `${bytes.format(value / 1024 ** 3)} GiB` : `${bytes.format(value / 1024 ** 2)} MiB`;
}

function BoxStatus({ machine, observation, now, connected }: { machine: Machine; observation?: BoxObservation; now: number; connected: boolean }) {
  const status = connected ? boxStatus(observation, now) : observation ? "stale" : "unknown";
  const health = observation?.health;
  const ready = status === "online" && health?.memoryReady && health.memoryTotalBytes > 0;
  const used = health ? health.memoryTotalBytes - health.memoryAvailableBytes : 0;
  const percent = ready ? Math.round(used / health.memoryTotalBytes * 100) : null;
  const memory = ready
    ? `${machine.name} memory: ${memoryBytes(used)} of ${memoryBytes(health.memoryTotalBytes)} in use`
    : `${machine.name} memory: ${status === "online" ? "unavailable" : status}`;
  return <div className="flex shrink-0 items-center gap-0.5">
    <PreviewCard>
      <PreviewCardTrigger render={<span tabIndex={0} />} className={footerControl} aria-label={`${machine.name}, ${status}. Machine details`}>
        <span aria-hidden="true" className={cn("size-1.5 rounded-full border border-current", status === "online" && "bg-current", status === "error" && "text-destructive-foreground")} />
        <span className="max-w-28 truncate">{machine.name}</span>
      </PreviewCardTrigger>
      <PreviewCardPopup align="end">
        <div className="flex w-64 min-w-0 flex-col gap-2.5">
          <div className="flex min-w-0 items-center gap-2">
            <OsLogo os={health?.os || machine.os} className="size-5" />
            <div className="flex min-w-0 flex-1 flex-col">
              <p className="truncate font-medium leading-tight">{machine.name}</p>
              <p className="truncate text-muted-foreground text-xs">{health?.hostname || machine.address || "Not observed yet"}</p>
            </div>
            <ArchChip architecture={health?.architecture ?? ""} />
          </div>
          <div className="flex items-center gap-2 text-xs">
            <span aria-hidden="true" className={cn("size-1.5 rounded-full", status === "online" ? "bg-success" : status === "error" ? "bg-destructive" : "bg-muted-foreground/60")} />
            <span className="capitalize">{status}</span>
            {observation && <span className="ms-auto text-muted-foreground">{new Date(health?.checkedAt || observation.attemptedAt).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" })}</span>}
          </div>
          {ready && percent !== null && (
            <div className="flex items-center gap-2 text-xs">
              <span className="text-muted-foreground">Memory</span>
              <Meter value={percent} aria-label={`${machine.name} physical memory used`} className="flex-1"><MeterTrack className="h-1"><MeterIndicator /></MeterTrack></Meter>
              <span className="tabular-nums">{memoryBytes(used)} / {memoryBytes(health.memoryTotalBytes)}</span>
            </div>
          )}
          {(observation?.error || health?.metricError) && <p className="text-destructive-foreground text-xs">{observation?.error || health?.metricError}</p>}
        </div>
      </PreviewCardPopup>
    </PreviewCard>
    <Tooltip>
      <TooltipTrigger render={<span tabIndex={0} />} className={footerControl} aria-label={memory}>
        {percent !== null ? <>
          <Meter value={percent} aria-label={`${machine.name} physical memory used`} className="w-8">
            <MeterTrack className="h-1"><MeterIndicator /></MeterTrack>
          </Meter>
          <span className="w-7 text-right tabular-nums">{percent}%</span>
        </> : <span className="text-muted-foreground">{status === "online" ? "n/a" : status}</span>}
      </TooltipTrigger>
      <TooltipPopup>{memory}</TooltipPopup>
    </Tooltip>
  </div>;
}

export function StatusBar() {
  const { link, health } = useDaemon();
  const counts = useAgentCounts();
  const agentsKnown = useAgents((state) => state.available && state.loaded && !state.offline && !state.error);
  const { machines, observations, loaded, error, now, mcp, mcpError } = useBoxes();
  const update = useUpdates((state) => state.status);
  useEffect(() => startBoxesPolling(), []);
  const connected = link === "connected";
  const online = connected && !error ? machines.filter((machine) => boxStatus(observations[machine.id], now) === "online").length : null;
  const unknown = machines.filter((machine) => boxStatus(observations[machine.id], now) === "unknown").length;
  return <footer aria-label="System status" className="flex h-7 min-w-0 shrink-0 items-center gap-2 border-t bg-sidebar px-3 font-mono text-xs text-muted-foreground">
    <span className="inline-flex shrink-0 items-center gap-1.5">
      <span aria-hidden="true" data-state={connected ? "connected" : link} className={cn("size-1.5 rounded-full border border-current", connected && "border-success bg-success", link === "offline" && "text-destructive-foreground")} />
      daemon {connected && health ? `v${health.version}` : link}
    </span>
    <Tooltip>
      <TooltipTrigger render={<button type="button" />} className={footerControl} onClick={() => useUI.getState().setView("agents")}>
        tasks <span className="tabular-nums">{connected && agentsKnown ? counts.working : "—"}</span>
      </TooltipTrigger>
      <TooltipPopup>Running agent tasks{counts.needsYou > 0 ? `; ${counts.needsYou} need your attention` : ""}</TooltipPopup>
    </Tooltip>
    <Tooltip>
      <TooltipTrigger render={<button type="button" />} className={footerControl} onClick={() => useUI.getState().openSettings("integrations")}>
        mcp <span className="tabular-nums">{connected && mcp && !mcpError ? `${mcp.filter((server) => server.status === "connected").length}/${mcp.length}` : "—"}</span>
      </TooltipTrigger>
      <TooltipPopup>{mcpError || "Connected / configured MCP servers"}</TooltipPopup>
    </Tooltip>
    <div className="flex min-w-0 flex-1 items-center justify-end gap-2 overflow-x-auto">
      {machines.map((machine) => <BoxStatus key={machine.id} machine={machine} observation={observations[machine.id]} now={now} connected={connected && !error} />)}
    </div>
    <Tooltip>
      <TooltipTrigger render={<span tabIndex={0} />} className={footerControl}>
        {loaded ? `${online ?? "—"}/${machines.length}` : "—"} online
      </TooltipTrigger>
      <TooltipPopup>{error || `${online ?? "Unknown"} online of ${machines.length} configured boxes${unknown ? `; ${unknown} not yet probed` : ""}. Stale, unknown and failed probes are not counted as offline.`}</TooltipPopup>
    </Tooltip>
    {update?.enabled && update.available && <Tooltip>
      <TooltipTrigger render={<button type="button" />} className={cn(footerControl, "text-foreground")} disabled={update.installing || update.checking} onClick={() => void useUpdates.getState().install()}>
        {update.installing ? "Updating…" : "Update"}
      </TooltipTrigger>
      <TooltipPopup>Update to {update.version}. Confirmation required: updating stops daemon-owned agents, terminals and automations.</TooltipPopup>
    </Tooltip>}
  </footer>;
}
