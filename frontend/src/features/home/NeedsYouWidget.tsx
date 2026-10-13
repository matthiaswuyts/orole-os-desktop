import { useState, type ReactNode } from "react";
import { ArrowUpRight, Check, Hand, ShieldAlert, Workflow } from "lucide-react";
import { useShallow } from "zustand/react/shallow";
import { Widget } from "@/components/widgets/Widget";
import type { WidgetProps } from "@/components/widgets/registry";
import { onDashboard } from "@/features/agents/columns";
import { ago, folderOf, sessionTitle, useNow } from "@/features/agents/format";
import { ProviderLogo } from "@/features/agents/ProviderLogo";
import { useApprovalsStore, useAutomationApprovals } from "@/features/automations/useApprovals";
import { botRequestTitle, isBotActor, usePolicyPending } from "@/features/mcp/usePolicyPending";
import { openTask } from "@/features/projects/tasks";
import type { Session } from "@/mygo";
import { useAgents } from "@/stores/agents";
import { useChat } from "@/stores/chat";
import { useUI } from "@/stores/ui";
import { useTasksStatus } from "./useTasksStatus";

/** What a waiting session is blocked on, in a few words. */
function waitingFor(s: Session): string {
  const permission = s.pending?.[0];
  if (permission) return `Permission: ${permission.summary || permission.tool}`;
  const question = s.questions?.[0]?.questions[0];
  if (question) return `Question: ${question.header || question.prompt}`;
  return s.activity || "Waiting for your reply";
}

function Row({ icon, title, detail, aside, onClick }: { icon: ReactNode; title: string; detail: string; aside: string; onClick?: () => void }) {
  const body = <>
    <span className="inline-flex size-8 shrink-0 items-center justify-center rounded-lg border bg-card text-muted-foreground [&_svg]:size-4" aria-hidden>{icon}</span>
    <span className="flex min-w-0 flex-1 flex-col gap-0.5">
      <span className="truncate text-[13px]">{title}</span>
      <span className="truncate text-muted-foreground text-xs">{detail}</span>
    </span>
    <span className="shrink-0 rounded-md bg-amber-500/15 px-1.5 py-0.5 text-[11px] text-amber-300">{aside}</span>
  </>;
  const row = "flex w-full items-center gap-3 px-3 py-2.5 text-left";
  return (
    <li>
      {onClick
        ? <button type="button" onClick={onClick} className={`${row} outline-none hover:bg-accent/60 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset`}>{body}</button>
        : <div className={row}>{body}</div>}
    </li>
  );
}

/** Tasks waiting on a permission, a question or a reply, MCP actions held for approval, and automation approvals. */
export function NeedsYouWidget({ onRemove }: WidgetProps) {
  const { status, message } = useTasksStatus();
  const sessions = useAgents(useShallow((s) => Object.values(s.sessions)
    .filter((session) => onDashboard(session) && (session.status === "needs-you" || session.pending?.length > 0 || (session.questions?.length ?? 0) > 0))
    .sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt))));
  const policy = usePolicyPending();
  const bots = useChat((s) => s.bots);
  const botName = (actor: string) => bots.find((b) => b.id === actor.slice(4))?.name ?? "a bot";
  const approvals = useAutomationApprovals();
  const approvalsCheckedAt = useApprovalsStore((s) => s.checkedAt);
  const [mountedAt] = useState(Date.now);
  const count = sessions.length + policy.items.length + approvals.length;
  const now = useNow(count === 0);
  const checkedAt = Math.max(mountedAt, policy.checkedAt, approvalsCheckedAt);
  return (
    <Widget title="Needs you" icon={<Hand />} status={status} message={message} onRemove={onRemove}
      count={count > 0 ? <span className="rounded-full bg-amber-500/15 px-1.5 text-[11px] text-amber-300 tabular-nums">{count}</span> : undefined}
      actions={<button type="button" aria-label="Open Tasks" title="Open Tasks" onClick={() => useUI.getState().setView("agents")}
        className="inline-flex size-6 shrink-0 items-center justify-center rounded-md text-muted-foreground outline-none hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"><ArrowUpRight className="size-3.5" /></button>}>
      {count === 0
        ? <div className="flex h-full min-h-40 flex-col items-center justify-center gap-1 px-4 py-6 text-center">
            <span className="mb-2 inline-flex size-9 items-center justify-center rounded-full border border-emerald-500/30 bg-emerald-500/10 text-emerald-400" aria-hidden><Check className="size-4" strokeWidth={2.5} /></span>
            <p className="font-medium text-[15px]">Nothing needs you right now</p>
            <p className="text-muted-foreground text-xs">Held actions and approvals show up here.</p>
            <p className="text-muted-foreground text-xs">Checked {ago(new Date(checkedAt).toISOString(), now)}.</p>
          </div>
        : <ul className="-mx-3 -my-1.5 flex flex-col divide-y divide-border/60">
            {sessions.map((s) => <Row key={s.id} icon={<ProviderLogo kind={s.kind} />} title={sessionTitle(s)} detail={[folderOf(s), waitingFor(s)].filter(Boolean).join(" · ")}
              aside={s.pending?.length ? "Permission" : s.questions?.length ? "Question" : "Reply"} onClick={() => openTask(s.id)} />)}
            {policy.items.map((p) => isBotActor(p.actor)
              ? <Row key={p.id} icon={<ShieldAlert />} title={botRequestTitle(p.action)} detail={`Asked by ${botName(p.actor)} · decide in that bot's chat`} aside="Held action" onClick={() => useChat.getState().open(p.actor.slice(4))} />
              : <Row key={p.id} icon={<ShieldAlert />} title={`${p.action} · ${p.resource}`} detail={`Requested by ${p.actor} · decide in the approval dialog`} aside="Held action" />)}
            {approvals.map((a) => <Row key={`${a.runId}:${a.stepId}`} icon={<Workflow />} title={a.name} detail={a.message || "Automation waiting for approval"} aside="Approval"
              onClick={() => useUI.getState().setView("automations")} />)}
          </ul>}
    </Widget>
  );
}
