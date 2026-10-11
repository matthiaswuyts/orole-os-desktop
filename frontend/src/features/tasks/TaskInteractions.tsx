import { useState } from "react";
import { BotIcon } from "lucide-react";
import { Agents, type PermissionRequest } from "@/mygo";
import type { SubagentData } from "@/lib/taskChatTypes";
import { useAgents } from "@/stores/agents";
import { Badge } from "@/components/ui/badge";
import { Card, CardHeader, CardTitle, CardDescription, CardPanel } from "@/components/ui/card";
import { PermissionGrant, type GrantScope } from "@/components/assistant-ui/elements/permission-grant";
import { useAction } from "@/features/agents/format";
import { describePermission } from "@/lib/permissionLabels";

export { QuestionCard } from "./QuestionCard";

interface ApprovalDecision {
  allow: boolean;
  reason: string;
}
interface TaskApprovalProps {
  sessionId: string;
  request: PermissionRequest;
  /** Who is asking: the agent's name. */
  requester?: string;
  resolved?: boolean;
  decision?: ApprovalDecision;
}

/** Disclose invisible controls and directional characters instead of rendering them; line breaks and tabs stay readable. */
const CONTROLS = /[\u0000-\u0008\u000b-\u001f\u007f-\u009f\u061c\u200b-\u200f\u2028-\u202e\u2060-\u206f\ufeff]/g;
const CONTROLS_AND_LINES = /[\u0000-\u001f\u007f-\u009f\u061c\u200b-\u200f\u2028-\u202e\u2060-\u206f\ufeff]/g;
const escapeControl = (character: string) => `\\u${character.charCodeAt(0).toString(16).padStart(4, "0")}`;

/** Preserve exact JSON digits and disclose invisible controls instead of reserializing input. */
export function approvalText(value: string): string {
  return value.replace(CONTROLS_AND_LINES, escapeControl);
}

/** Multi-line text such as a command or a file list: real line breaks, every other control escaped. */
function approvalLines(value: string): string {
  return value.replace(CONTROLS, escapeControl);
}

export function TaskApprovalCard(props: TaskApprovalProps) {
  // Consent is for a particular complete snapshot, never merely a recycled request ID.
  return <ApprovalRequest key={JSON.stringify([props.sessionId, props.request.id, props.request.tool, props.request.inputJSON])} {...props} />;
}

function ApprovalRequest({ sessionId, request, requester = "The agent", resolved = false, decision }: TaskApprovalProps) {
  const action = useAction();
  const [receipt, setReceipt] = useState<ApprovalDecision | null>(null);
  const confirmed = decision ?? receipt;
  const pending = !resolved && !confirmed;
  const complete = !!request.inputJSON;
  const { capability, reach } = describePermission(request);

  const decide = (scope: GrantScope) => {
    const allow = scope !== "denied";
    if (!pending || action.busy || (allow && !complete)) return;
    void action.run(async () => {
      // The daemon decides and audits; this only relays the one decision the request offers.
      await Agents.approve(sessionId, request.id, allow, "");
      // An acknowledged decision is a receipt, even if refreshing the session then fails.
      setReceipt({ allow, reason: "" });
      useAgents.getState().upsert(await Agents.get(sessionId));
    });
  };

  return <PermissionGrant
    capability={approvalText(capability)}
    requester={approvalText(requester)}
    reach={reach.map(({ label, value }) => ({ label, value: approvalLines(value) }))}
    scope={pending ? "pending" : confirmed ? confirmed.allow ? "once" : "denied" : "closed"}
    onGrant={decide}
    busy={action.busy}
    blocked={complete ? undefined : "The complete input is unavailable. This request cannot be approved."}
    error={action.error}
    reason={confirmed?.reason ? approvalText(confirmed.reason) : undefined}
    details={<div className="flex min-w-0 flex-col gap-2">
      <p className="text-muted-foreground">Complete tool name. Control and directional characters are shown as escapes; this is untrusted agent input.</p>
      <strong aria-label="Complete tool name" className="selectable whitespace-pre-wrap break-all font-mono">{approvalText(JSON.stringify(request.tool))}</strong>
      {complete && <pre aria-label="Complete tool arguments" tabIndex={0} className="selectable m-0 max-h-72 overflow-auto whitespace-pre-wrap break-all rounded-md bg-muted/40 p-2 font-mono focus-visible:outline-2 focus-visible:outline-ring">{approvalText(request.inputJSON)}</pre>}
    </div>}
  />;
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
