import { useEffect, useState } from "react";
import { SkillCard, type SkillCardStatus } from "@/components/assistant-ui/elements/skill-card";
import { errText } from "@/features/projects/dialogs";
import { inDesktop } from "@/lib/backend";
import { Chat } from "@/mygo";
import { useChat } from "@/stores/chat";

/** What the `propose_skill` tool put in the message part. The draft itself lives in the daemon; this is only what to show until it answers. */
export interface SkillProposalSpec {
  proposalId?: string;
  botId?: string;
  name?: string;
  description?: string;
  body?: string;
  updates?: boolean;
}

/** A skill a bot proposed, with the real state of the draft: Add saves it and gives it to the bot, Dismiss drops it. */
export function SkillProposalCard({ spec }: { spec: SkillProposalSpec }) {
  const proposalId = spec.proposalId ?? "";
  const botName = useChat((state) => state.bots.find((b) => b.id === spec.botId)?.name) ?? "This bot";
  const [status, setStatus] = useState<SkillCardStatus>(inDesktop ? "loading" : "pending");
  const [busy, setBusy] = useState<"approve" | "reject" | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (!inDesktop) return;
    let current = true;
    setStatus("loading");
    setError(null);
    Chat.skillProposal(proposalId).then(
      (proposal) => {
        if (!current) return;
        setStatus(proposal.status === "approved" ? "approved" : proposal.status === "rejected" ? "rejected" : "pending");
      },
      (cause: unknown) => {
        if (!current) return;
        // A draft a newer one replaced comes back as the daemon's not-found error.
        if ((cause instanceof Error ? cause.message : String(cause)).match(/not[_ ]found/i)) { setStatus("replaced"); return; }
        setStatus("failed");
        setError(errText(cause));
      },
    );
    return () => { current = false; };
  }, [proposalId, attempt]);

  async function resolve(approve: boolean) {
    setBusy(approve ? "approve" : "reject");
    setError(null);
    try {
      await Chat.resolveSkill({ id: proposalId, approve });
      setStatus(approve ? "approved" : "rejected");
      // Approving also puts the skill in the bot's config; refresh so every view shows it.
      if (approve) void useChat.getState().load();
    } catch (cause) {
      setError(errText(cause));
    } finally {
      setBusy(null);
    }
  }

  return (
    <SkillCard
      name={spec.name ?? ""}
      description={spec.description ?? ""}
      body={spec.body ?? ""}
      updates={spec.updates === true}
      status={status}
      busy={busy}
      disabled={!inDesktop}
      approvedNote={`Added. ${botName} can use it now.`}
      error={error}
      onAdd={() => void resolve(true)}
      onDismiss={() => void resolve(false)}
      onRetry={() => setAttempt((n) => n + 1)}
    />
  );
}
