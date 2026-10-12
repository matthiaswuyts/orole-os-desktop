import { CircleHelp, RefreshCw } from "lucide-react";
import type { SyncState } from "@/mygo";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Popover, PopoverDescription, PopoverPopup, PopoverTitle, PopoverTrigger } from "@/components/ui/popover";
import { Spinner } from "@/components/ui/spinner";
import { cn } from "@/lib/utils";
import { agentStatus, harnessLabel, needsSync } from "./skills";

const DOT = { success: "bg-success", warning: "bg-warning", error: "bg-destructive", outline: "bg-muted-foreground/40" } as const;

/**
 * One line that answers "will my agents see my skills?". Skills live in Orole and every agent
 * reads the enabled ones through the Orole connection; Claude Code, Codex and Oh My Pi also read a
 * skills folder of their own, and "Update agents" copies the enabled skills there. The numbers,
 * folders and the connection entry sit in the popover for anyone who wants them.
 */
export function HarnessStatus({ states, error, syncing, onSync, onRetry }: {
  states: SyncState[] | null;
  error: string | null;
  syncing: string | null;
  onSync: (harness: string) => void;
  onRetry: () => void;
}) {
  const installed = (states ?? []).filter((s) => s.available);
  const stale = installed.filter(needsSync);
  const busy = syncing !== null;
  return <section aria-label="Agents" className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1.5 border-b bg-sidebar/40 px-4 py-2">
    <span className="font-medium text-xs">Used by</span>
    {error && <p role="alert" className="text-destructive-foreground text-xs">{error} <button className="underline" onClick={onRetry}>Retry</button></p>}
    {!states && !error && <span role="status" className="flex items-center gap-2 text-muted-foreground text-xs"><Spinner className="size-3.5" />Checking agents…</span>}
    {states && installed.length === 0 && <span className="text-muted-foreground text-xs">No coding agent found. Skills still reach any agent that Orole starts.</span>}
    <ul className="flex flex-wrap items-center gap-1.5">
      {installed.map((s) => {
        const status = agentStatus(s);
        return <li key={s.harness} className="flex items-center gap-1.5 rounded-full border bg-card py-0.5 pr-2.5 pl-2 text-xs" title={status.label}>
          <span aria-hidden className={cn("size-1.5 rounded-full", DOT[status.tone])} />
          <span>{s.name || harnessLabel(s.harness)}</span>
          <span className="sr-only">{status.label}</span>
        </li>;
      })}
    </ul>
    <div className="ml-auto flex items-center gap-2">
      {states && installed.length > 0 && (stale.length > 0
        ? <Button size="xs" disabled={busy} onClick={() => onSync("")}>{syncing !== null ? <Spinner data-icon="inline-start" /> : <RefreshCw data-icon="inline-start" />}Update {stale.length === 1 ? "agent" : `${stale.length} agents`}</Button>
        : <span className="text-muted-foreground text-xs">All agents up to date</span>)}
      <Popover>
        <PopoverTrigger render={<Button size="icon-xs" variant="ghost" aria-label="How skills reach your agents" />}><CircleHelp /></PopoverTrigger>
        <PopoverPopup align="end" className="w-96">
          <PopoverTitle>How skills reach your agents</PopoverTitle>
          <PopoverDescription render={<div />} className="mt-1 flex flex-col gap-2">
            <p>Your skills live in Orole. Turn a skill off here and no agent sees it.</p>
            <p>Every agent started from Orole reads your enabled skills through the Orole connection. Nothing to do.</p>
            <p>Claude Code, Codex and Oh My Pi also look in a skills folder of their own. <strong>Update</strong> copies your enabled skills into it, so they work when you run those tools yourself.</p>
          </PopoverDescription>
          <ul className="mt-3 flex flex-col divide-y rounded-lg border">
            {(states ?? []).map((s) => {
              const status = agentStatus(s);
              const pending = (s.outdated?.length ?? 0) + (s.missing?.length ?? 0);
              return <li key={s.harness} className="flex flex-col gap-1 p-2">
                <div className="flex items-center gap-2">
                  <span className="font-medium">{s.name || harnessLabel(s.harness)}</span>
                  <Badge size="sm" variant={status.tone}>{status.label}</Badge>
                  {s.available && needsSync(s) && <Button size="xs" variant="outline" className="ml-auto" disabled={busy} aria-label={`Update ${s.name || harnessLabel(s.harness)}`} onClick={() => onSync(s.harness)}>{syncing === s.harness ? <Spinner data-icon="inline-start" /> : <RefreshCw data-icon="inline-start" />}Update</Button>}
                </div>
                {s.available
                  ? <p className="text-muted-foreground text-xs">{s.synced?.length ?? 0} skills copied{pending > 0 ? `, ${pending} to update` : ""}{s.gateway === "installed" ? "" : s.gateway === "unsupported" ? "" : ", Orole connection needs setup"}</p>
                  : <p className="text-muted-foreground text-xs">Install {s.name || harnessLabel(s.harness)} to use your skills in it.</p>}
                {s.available && s.dir && <p className="selectable truncate font-mono text-muted-foreground text-xs" title={s.dir}>{s.dir}</p>}
                {s.error && <p role="alert" className="break-words text-destructive-foreground text-xs">{s.error}</p>}
              </li>;
            })}
          </ul>
        </PopoverPopup>
      </Popover>
    </div>
  </section>;
}
