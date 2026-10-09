import { useEffect, useState } from "react";
import { Check, X } from "lucide-react";
import { Chat, events, type SkillProposal } from "@/mygo";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { errText } from "@/features/projects/dialogs";
import { Section } from "./controls";
import { useLoad } from "./useLoad";

const PREVIEW_LINES = 12;

/** The proposed SKILL.md shown as added lines: nothing exists yet, so every line is new. */
function BodyDiff({ body, expanded }: { body: string; expanded: boolean }) {
  const lines = body.replace(/\r\n/g, "\n").split("\n");
  const shown = expanded ? lines : lines.slice(0, PREVIEW_LINES);
  return (
    <pre aria-label="Proposed skill text" className="selectable max-h-80 overflow-auto rounded-md border bg-background p-0 font-mono text-xs leading-5">
      {shown.map((line, i) => (
        <span key={i} className="flex bg-success/6">
          <span aria-hidden="true" className="w-6 shrink-0 select-none text-center text-success-foreground">+</span>
          <span className="min-w-0 flex-1 whitespace-pre-wrap break-words pe-2">{line || " "}</span>
        </span>
      ))}
    </pre>
  );
}

function Proposal({ proposal, onResolved }: { proposal: SkillProposal; onResolved: () => void }) {
  const [expanded, setExpanded] = useState(false);
  const [busy, setBusy] = useState<"approve" | "reject" | null>(null);
  const [error, setError] = useState<string | null>(null);
  const long = proposal.body.split("\n").length > PREVIEW_LINES;

  async function resolve(approve: boolean) {
    setBusy(approve ? "approve" : "reject");
    setError(null);
    try {
      await Chat.resolveSkill({ id: proposal.id, approve });
      onResolved();
    } catch (cause) {
      setError(errText(cause));
    } finally {
      setBusy(null);
    }
  }

  return (
    <li className="flex min-w-0 flex-col gap-2 rounded-md border bg-background/40 p-2.5">
      <div className="flex flex-col gap-0.5">
        <span className="selectable break-words font-medium text-sm">{proposal.name}</span>
        {proposal.description && <span className="selectable break-words text-muted-foreground text-xs">{proposal.description}</span>}
      </div>
      <BodyDiff body={proposal.body} expanded={expanded} />
      {long && <Button type="button" size="xs" variant="ghost" className="self-start" onClick={() => setExpanded((e) => !e)}>{expanded ? "Show less" : "Show full text"}</Button>}
      {error && <p role="alert" className="text-destructive-foreground text-xs">{error}</p>}
      <div className="flex gap-2">
        <Button type="button" size="xs" disabled={busy !== null} onClick={() => void resolve(true)} aria-label={`Approve skill ${proposal.name}`}><Check />Approve</Button>
        <Button type="button" size="xs" variant="outline" disabled={busy !== null} onClick={() => void resolve(false)} aria-label={`Reject skill ${proposal.name}`}><X />Reject</Button>
      </div>
    </li>
  );
}

/** Skills the bot proposed. They are inert text until approved. */
export function SkillProposals({ botId }: { botId: string }) {
  const proposals = useLoad(() => Chat.skillProposals(botId), botId);
  const { reload } = proposals;
  useEffect(() => events.chatMemory.on(() => reload()), [reload]);
  const pending = (proposals.data ?? []).filter((p) => p.status === "pending");
  return (
    <Section title="Proposed skills" description="A bot can suggest a skill it wants to keep using. Nothing is active until you approve it; approving adds it to your skills library.">
      {proposals.error && (
        <Alert variant="error"><AlertDescription className="flex items-center justify-between gap-2"><span className="selectable break-words">Could not load proposals: {proposals.error}</span><Button size="xs" variant="outline" onClick={reload}>Try again</Button></AlertDescription></Alert>
      )}
      {!proposals.error && pending.length === 0 && <p className="text-muted-foreground text-xs">{proposals.loading ? "Loading…" : "No skills are waiting for your approval."}</p>}
      {pending.length > 0 && <ul className="flex flex-col gap-2">{pending.map((p) => <Proposal key={p.id} proposal={p} onResolved={reload} />)}</ul>}
    </Section>
  );
}
