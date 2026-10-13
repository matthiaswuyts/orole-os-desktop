import { useEffect, useState } from "react";
import { Policy, type Pending, type Rule } from "@/mygo";
import { PermissionGrant, type GrantScope } from "@/components/assistant-ui/elements/permission-grant";
import { useAction } from "@/features/agents/format";
import { botRequestTitle, usePolicyPending } from "@/features/mcp/usePolicyPending";
import { useChat } from "@/stores/chat";

/** The actions a bot needs to run code tasks; "always" remembers both so a follow-up never asks again after a start was trusted. */
const BOT_TASK_ACTIONS = ["agent.start", "agent.prompt"] as const;

function alwaysRule(actor: string, action: string): Rule {
  return { id: 0, actor, action, resource: "*", maxEffect: "write", decision: "allow", note: "always allow from chat", createdAt: 0 };
}

/**
 * One held bot request (start a task, send a follow-up), decided in the conversation it came from. The decision is a receipt
 * kept while the daemon still lists the request; the parent unmounts the card once the queue no longer has it.
 */
export function JobApprovalCard({ request, requester }: { request: Pending; requester: string }) {
  const action = useAction();
  const [receipt, setReceipt] = useState<GrantScope | null>(null);
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    if (receipt) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [receipt]);
  const expired = now >= request.expiresAt;
  const decide = (scope: GrantScope) => {
    if (receipt || action.busy) return;
    void action.run(async () => {
      if (scope === "always") for (const name of BOT_TASK_ACTIONS) await Policy.addRule(alwaysRule(request.actor, name));
      await Policy.resolve(request.id, scope !== "denied", false);
      setReceipt(scope);
    });
  };
  return <PermissionGrant
    data-request={request.id}
    capability={botRequestTitle(request.action)}
    requester={requester}
    reach={request.detail ? [request.detail] : []}
    scope={receipt ?? "pending"}
    onGrant={decide}
    grants={request.canRemember ? ["once", "always"] : ["once"]}
    busy={action.busy}
    blocked={expired ? "This request has expired. It cannot authorize the action; the bot has to ask again." : undefined}
    error={action.error}
    details={<dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
      <dt className="text-muted-foreground">Actor</dt><dd className="selectable break-all font-mono">{request.actor}</dd>
      <dt className="text-muted-foreground">Action</dt><dd className="selectable break-all font-mono">{request.action}</dd>
      <dt className="text-muted-foreground">Resource</dt><dd className="selectable break-all font-mono">{request.resource}</dd>
      {request.reason && <><dt className="text-muted-foreground">Why</dt><dd className="selectable whitespace-pre-wrap break-words">{request.reason}{request.fromHint ? " The effect is based on untrusted annotations." : ""}</dd></>}
      <dt className="text-muted-foreground">Expires</dt><dd>{new Date(request.expiresAt).toLocaleTimeString()}</dd>
    </dl>}
  />;
}

/** Every held request from this bot, above the composer. "Always allow" is listed under Settings › Task permissions. */
export function BotApprovals({ botId }: { botId: string }) {
  const { items } = usePolicyPending();
  const name = useChat((s) => s.bots.find((bot) => bot.id === botId)?.name) ?? "This bot";
  const mine = items.filter((item) => item.actor === `bot:${botId}`);
  if (mine.length === 0) return null;
  return <div data-slot="bot-approvals" role="region" aria-label={`Approvals for ${name}`} className="mx-auto flex w-full max-w-3xl flex-col gap-2 px-4 pb-2">
    {mine.map((item) => <JobApprovalCard key={item.id} request={item} requester={name} />)}
  </div>;
}
