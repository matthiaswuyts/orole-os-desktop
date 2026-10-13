import { useEffect, useState } from "react";
import { CheckIcon, TriangleAlertIcon, WorkflowIcon } from "lucide-react";
import { paper } from "@/components/assistant-ui/elements/surfaces";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { blockSummary, flatten, triggerSummary } from "@/features/automations/flow";
import { errText } from "@/features/projects/dialogs";
import { inDesktop } from "@/lib/backend";
import { cn } from "@/lib/utils";
import { Automations, type Proposal, type StepType } from "@/mygo";
import { useChat } from "@/stores/chat";
import { useUI } from "@/stores/ui";

/** What the `propose_automation` tool put in the message part. The draft itself lives in the daemon. */
export interface AutomationProposalSpec {
  proposalId?: string;
  botId?: string;
  name?: string;
}

type Busy = "on" | "off" | "dismiss" | null;
type Phase = "loading" | "ready" | "failed";

const NOTES: Record<string, string> = {
  rejected: "Dismissed",
  replaced: "Replaced by a newer draft",
};

/**
 * An automation a bot drafted, drawn from the daemon's stored draft (never from the bot's words):
 * its trigger, its blocks and anything that runs programs or sends requests. Nothing exists until
 * the user decides here; drafts with such effects can only be added turned off, for review.
 */
export function AutomationProposalCard({ spec }: { spec: AutomationProposalSpec }) {
  const proposalId = spec.proposalId ?? "";
  const bots = useChat((state) => state.bots);
  const [proposal, setProposal] = useState<Proposal | null>(null);
  const [types, setTypes] = useState<StepType[]>([]);
  const [phase, setPhase] = useState<Phase>(inDesktop ? "loading" : "failed");
  const [busy, setBusy] = useState<Busy>(null);
  const [error, setError] = useState<string | null>(inDesktop ? null : "Open Orole to review this automation.");
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (!inDesktop) return;
    let current = true;
    setPhase("loading");
    setError(null);
    Promise.all([Automations.proposal(proposalId), Automations.stepTypes().catch(() => [] as StepType[])]).then(
      ([p, t]) => { if (current) { setProposal(p); setTypes(t ?? []); setPhase("ready"); } },
      (cause: unknown) => { if (current) { setPhase("failed"); setError(errText(cause)); } },
    );
    return () => { current = false; };
  }, [proposalId, attempt]);

  async function decide(next: Busy) {
    if (!proposal) return;
    setBusy(next);
    setError(null);
    try {
      const enable = proposal.replaces ? proposal.replacesEnabled && proposal.risks.length === 0 : next === "on";
      setProposal(await Automations.resolveProposal({ id: proposal.id, approve: next !== "dismiss", enable }));
    } catch (cause) {
      setError(errText(cause));
    } finally {
      setBusy(null);
    }
  }

  const name = proposal?.definition.name || spec.name || "Automation";
  const names = { bot: (id: string) => bots.find((b) => b.id === id)?.name, project: () => undefined };
  const label = (type: string) => types.find((t) => t.type === type)?.label ?? type;
  const steps = proposal ? flatten(proposal.definition.steps) : [];
  const risky = (proposal?.risks.length ?? 0) > 0;
  const status = proposal?.status ?? "pending";
  const locked = busy !== null;

  return (
    <div role="group" aria-label={`Automation ${name}`} data-slot="automation-card" data-status={phase === "ready" ? status : phase}
      className={cn(paper, "flex w-full max-w-lg min-w-0 flex-col gap-3 rounded-2xl p-4")}>
      <div className="flex items-start gap-3">
        <span aria-hidden="true" className="flex size-9 shrink-0 items-center justify-center rounded-lg border bg-muted/40 text-muted-foreground">
          <WorkflowIcon className="size-4.5" />
        </span>
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <span className="selectable min-w-0 break-words text-[13px] font-medium">{name}</span>
            <Badge variant={proposal?.replaces ? "info" : "secondary"} size="sm">{proposal?.replaces ? "Update" : "New automation"}</Badge>
          </div>
          {proposal?.rationale && <p className="selectable break-words text-[13px] text-muted-foreground">{proposal.rationale}</p>}
        </div>
      </div>

      {proposal && (
        <div className="flex flex-col gap-2 text-[13px]">
          <p><span className="text-muted-foreground">Starts: </span>{triggerSummary(proposal.definition.trigger, [])}</p>
          <ol aria-label="Steps" className="flex flex-col gap-1 border-s ps-3">
            {steps.map(({ step, depth }) => (
              <li key={step.id} className="min-w-0 break-words" style={{ paddingInlineStart: `${depth * 12}px` }}>
                <span className="font-medium">{label(step.type)}</span>
                {blockSummary(step, names) && <span className="text-muted-foreground"> · {blockSummary(step, names)}</span>}
              </li>
            ))}
          </ol>
          {risky && (
            <div role="note" aria-label="Review before it runs" className="flex flex-col gap-1 rounded-lg border border-warning/40 bg-warning/8 p-2.5 text-warning-foreground">
              <span className="flex items-center gap-1.5 text-xs font-medium"><TriangleAlertIcon aria-hidden="true" className="size-3.5" />Review in Automations before it runs</span>
              <ul className="flex flex-col gap-0.5 text-xs">
                {proposal.risks.map((r) => <li key={r} className="selectable break-words font-medium">{r}</li>)}
              </ul>
            </div>
          )}
        </div>
      )}

      <div className="flex flex-col gap-2">
        <p role="status" className={cn("flex items-center gap-1.5 text-xs", status === "approved" ? "text-success-foreground" : "text-muted-foreground")}>
          {phase === "loading" && "Checking this draft…"}
          {phase === "ready" && status === "approved" && <><CheckIcon aria-hidden="true" className="size-3.5 shrink-0" />{approvedNote(proposal!)}</>}
          {phase === "ready" && NOTES[status]}
        </p>
        {error && <p role="alert" className="selectable break-words text-xs text-destructive-foreground">{error}</p>}
        {phase === "ready" && status === "pending" && (
          <div className="flex flex-wrap justify-end gap-2">
            <Button type="button" size="sm" variant="ghost" disabled={locked} loading={busy === "dismiss"} onClick={() => void decide("dismiss")}>Dismiss</Button>
            {proposal?.replaces ? (
              <Button type="button" size="sm" disabled={locked} loading={busy === "on"} onClick={() => void decide("on")}>Apply changes</Button>
            ) : <>
              <Button type="button" size="sm" variant={risky ? "default" : "outline"} disabled={locked} loading={busy === "off"} onClick={() => void decide("off")}>Add turned off</Button>
              {!risky && <Button type="button" size="sm" disabled={locked} loading={busy === "on"} onClick={() => void decide("on")}>Turn on</Button>}
            </>}
          </div>
        )}
        {phase === "ready" && status === "approved" && proposal?.automationId && (
          <div className="flex justify-end">
            <Button type="button" size="sm" variant="outline" onClick={() => useUI.getState().focusAutomation(proposal.automationId)}>Open in Automations</Button>
          </div>
        )}
        {phase === "failed" && inDesktop && (
          <div className="flex justify-end">
            <Button type="button" size="sm" variant="outline" onClick={() => setAttempt((n) => n + 1)}>Retry</Button>
          </div>
        )}
      </div>
    </div>
  );
}

/** The outcome line of an approved draft; `definition.enabled` is the saved automation's current switch. */
function approvedNote(p: Proposal): string {
  if (p.replaces) return p.definition.enabled ? "Changes applied." : "Changes applied. It is turned off; review it in Automations to turn it on.";
  return p.definition.enabled ? "Turned on." : "Added turned off. Review it in Automations to turn it on.";
}
