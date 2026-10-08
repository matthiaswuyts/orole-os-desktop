import { useState } from "react";
import { Bell, CircleDot, GitPullRequest, PlayCircle, MessageSquare } from "lucide-react";
import { GitHub } from "@/mygo";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { EmptyLine, ErrorBox, Header, Loading, RefreshButton, agoIso, parseSubjectUrl, useAsync } from "./shared";
import { useGitHub } from "./store";

const ICON: Record<string, typeof Bell> = { Issue: CircleDot, PullRequest: GitPullRequest, CheckSuite: PlayCircle, Discussion: MessageSquare };

export function NotificationsTab() {
  const [all, setAll] = useState(false);
  const open = useGitHub((s) => s.open);
  const a = useAsync(() => GitHub.notifications(all, 50), [all]);
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <Header title="Notifications" count={a.data && <Badge size="sm" variant="outline">{a.data.length}</Badge>}>
        <label className="flex items-center gap-1.5 text-muted-foreground text-xs">Include read<Switch checked={all} onCheckedChange={setAll} aria-label="Include read notifications" /></label>
        <RefreshButton a={a} />
      </Header>
      <div className="min-h-0 flex-1 overflow-auto">
        {a.error && <ErrorBox error={a.error} onRetry={a.reload} />}
        {a.loading && !a.data && <Loading what="notifications" />}
        {a.data?.length === 0 && <EmptyLine>{all ? "No notifications." : "You are all caught up."}</EmptyLine>}
        <ul className="divide-y">
          {a.data?.map((n) => {
            const Icon = ICON[n.subject.type] ?? Bell;
            const target = parseSubjectUrl(n.subject.url);
            return (
              <li key={n.id}>
                <button disabled={!target} onClick={() => target && open(target)} className="flex w-full items-start gap-2 px-3 py-2 text-left outline-none hover:bg-accent/50 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset disabled:cursor-default disabled:hover:bg-transparent">
                  <Icon className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                  <span className="min-w-0 flex-1">
                    <span className={n.unread ? "block text-sm font-medium" : "block text-sm text-muted-foreground"}>{n.subject.title}</span>
                    <span className="text-[11px] text-muted-foreground">{n.repository.full_name} · {n.reason.replace("_", " ")} · {agoIso(n.updated_at)}</span>
                  </span>
                  {n.unread && <span className="mt-1.5 size-2 shrink-0 rounded-full bg-primary" aria-label="unread" />}
                </button>
              </li>
            );
          })}
        </ul>
      </div>
    </div>
  );
}
