import { useId, useState } from "react";
import { BotIcon, ShieldCheckIcon } from "lucide-react";
import { Agents, type PermissionRequest } from "@/mygo";
import type { SubagentData } from "@/lib/taskChatTypes";
import { useAgents } from "@/stores/agents";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardHeader, CardTitle, CardDescription, CardPanel, CardFooter } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { useAction } from "@/features/agents/format";

export { QuestionCard } from "./QuestionCard";

interface ApprovalDecision {
  allow: boolean;
  reason: string;
}
interface TaskApprovalProps {
  sessionId: string;
  request: PermissionRequest;
  resolved?: boolean;
  decision?: ApprovalDecision;
}

/** Preserve exact JSON digits and disclose invisible controls instead of reserializing input. */
function approvalText(value: string): string {
  return value.replace(/[\u0000-\u001f\u007f-\u009f\u061c\u200b-\u200f\u2028-\u202e\u2060-\u206f\ufeff]/g,
    (character) => `\\u${character.charCodeAt(0).toString(16).padStart(4, "0")}`);
}

export function TaskApprovalCard(props: TaskApprovalProps) {
  // Consent is for a particular complete snapshot, never merely a recycled request ID.
  return <ApprovalRequest key={JSON.stringify([props.sessionId, props.request.id, props.request.tool, props.request.inputJSON])} {...props} />;
}

function ApprovalRequest({ sessionId, request, resolved = false, decision }: TaskApprovalProps) {
  const action = useAction();
  const titleId = useId();
  const reasonId = useId();
  const [reviewed, setReviewed] = useState(false);
  const [reason, setReason] = useState("");
  const [receipt, setReceipt] = useState<ApprovalDecision | null>(null);
  const confirmed = decision ?? receipt;
  const pending = !resolved && !confirmed;
  const complete = !!request.inputJSON;

  const decide = (allow: boolean) => {
    if (!pending || action.busy || (allow && (!complete || !reviewed))) return;
    void action.run(async () => {
      await Agents.approve(sessionId, request.id, allow, reason);
      // An acknowledged decision is a receipt, even if refreshing the session then fails.
      setReceipt({ allow, reason });
      useAgents.getState().upsert(await Agents.get(sessionId));
    });
  };

  return <Card aria-labelledby={titleId} className="min-w-0">
    <CardHeader>
      <CardTitle id={titleId} className="flex items-center gap-2"><ShieldCheckIcon aria-hidden className="size-4" />Tool permission</CardTitle>
      <CardDescription>{pending ? "Review this action before granting permission. Approval applies only to this request." : "Permission request history"}</CardDescription>
    </CardHeader>
    <CardPanel className="flex flex-col gap-3">
      <strong aria-label="Complete tool name" className="selectable whitespace-pre-wrap break-all font-mono text-sm">{approvalText(JSON.stringify(request.tool))}</strong>
      <p className="text-xs text-muted-foreground">Complete tool arguments. Control and directional characters are shown as escapes; this is untrusted agent input.</p>
      {complete ? <pre aria-label="Complete tool arguments" tabIndex={0} className="selectable max-h-72 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-muted/40 p-3 font-mono text-xs focus-visible:outline-2 focus-visible:outline-ring">{approvalText(request.inputJSON)}</pre>
        : <p role="alert" className="text-xs text-destructive-foreground">The complete input is unavailable. This request cannot be approved.</p>}
      {pending && <>
        <label className="flex min-h-9 items-start gap-2 text-sm"><Checkbox checked={reviewed} disabled={!complete || action.busy} onCheckedChange={setReviewed} />I reviewed the complete tool name and arguments.</label>
        <Field>
          <FieldLabel htmlFor={reasonId}>Reason (optional)</FieldLabel>
          <Input id={reasonId} value={reason} disabled={action.busy} onChange={(event) => setReason(event.target.value)} />
        </Field>
      </>}
    </CardPanel>
    <CardFooter className="flex flex-col items-start gap-2">
      {pending ? <div className="flex flex-wrap gap-2">
        <Button type="button" size="sm" disabled={!complete || !reviewed || action.busy} loading={action.busy} onClick={() => decide(true)}>Allow once</Button>
        <Button type="button" size="sm" variant="outline" disabled={action.busy} onClick={() => decide(false)}>Deny</Button>
      </div> : <div role="status" className="flex flex-col gap-2 text-xs">
        <Badge variant={confirmed ? confirmed.allow ? "success" : "secondary" : "outline"}>{confirmed ? confirmed.allow ? "Allowed once" : "Denied" : "No longer pending"}</Badge>
        <p>{confirmed ? "Decision acknowledged. Tool execution status is reported separately." : "No decision receipt is available for this request."}</p>
        {confirmed?.reason && <p className="whitespace-pre-wrap break-words">Reason: {approvalText(confirmed.reason)}</p>}
      </div>}
      {action.error && <p role="alert" className="whitespace-pre-wrap break-words text-xs text-destructive-foreground">{action.error}</p>}
    </CardFooter>
  </Card>;
}

/** IDs, task text and status come only from provider events; they are not Orole session links. */
export function SubagentCard({ agent }: { agent: SubagentData }) {
  return <Card className="min-w-0">
    <CardHeader>
      <CardTitle className="flex min-w-0 items-start gap-2"><BotIcon aria-hidden className="size-4 shrink-0" /><span className="min-w-0 break-words">{agent.name || agent.id}</span></CardTitle>
      <CardDescription>Provider-reported agent</CardDescription>
    </CardHeader>
    <CardPanel className="flex flex-col gap-3 text-sm">
      <div className="flex flex-wrap items-center gap-2"><Badge variant="secondary">{agent.status || "Status not reported"}</Badge><span className="selectable break-all font-mono text-xs">{agent.id}</span></div>
      {agent.task && <p className="whitespace-pre-wrap break-words">{agent.task}</p>}
      {agent.message && <p className="whitespace-pre-wrap break-words text-muted-foreground">{agent.message}</p>}
      {agent.toolCallId && <p className="break-all text-xs text-muted-foreground">Tool call: <span className="font-mono">{agent.toolCallId}</span></p>}
    </CardPanel>
  </Card>;
}
