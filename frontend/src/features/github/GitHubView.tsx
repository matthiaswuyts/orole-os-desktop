import { useEffect } from "react";
import { Bell, FolderGit2, GitPullRequest, CircleDot, PlayCircle } from "lucide-react";
import { GitHub } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { NeedsDesktop } from "@/features/shared/States";
import { Select, SelectGroup, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { cn } from "@/lib/utils";
import { ActionsTab } from "./ActionsTab";
import { IssuesTab } from "./IssuesTab";
import { NotificationsTab } from "./NotificationsTab";
import { PullsTab } from "./PullsTab";
import { ReposTab } from "./ReposTab";
import { SetupScreen } from "./SetupScreen";
import { useAsync } from "./shared";
import { useGitHub, type GitHubTab } from "./store";

const TABS: { id: GitHubTab; label: string; icon: typeof Bell }[] = [
  { id: "repos", label: "Repositories", icon: FolderGit2 },
  { id: "issues", label: "Issues", icon: CircleDot },
  { id: "pulls", label: "Pull requests", icon: GitPullRequest },
  { id: "notifications", label: "Notifications", icon: Bell },
  { id: "actions", label: "Actions", icon: PlayCircle },
];

function RepoPicker() {
  const { repo, setRepo } = useGitHub();
  const repos = useAsync(() => GitHub.repos("", 100), []);
  const names = [...new Set([...(repo ? [repo] : []), ...(repos.data ?? []).map((r) => r.full_name)])];
  return (
    <Select value={repo || null} onValueChange={(v) => v && setRepo(String(v))}>
      <SelectTrigger size="sm" aria-label="Repository" className="w-64"><SelectValue placeholder="Choose repository…" /></SelectTrigger>
      <SelectPopup><SelectGroup>{names.map((n) => <SelectItem key={n} value={n}>{n}</SelectItem>)}</SelectGroup></SelectPopup>
    </Select>
  );
}

export function GitHubView() {
  const { status, error, tab, repo, setTab, load, startSync } = useGitHub();
  useEffect(() => { void load(); return startSync(); }, [load, startSync]);

  if (!inDesktop) return <NeedsDesktop what="GitHub" />;
  if (!status && !error) return <div role="status" className="flex flex-1 items-center justify-center gap-2 text-muted-foreground text-sm"><Spinner className="size-4" />Checking GitHub connection…</div>;
  if (!status?.connected) return <SetupScreen />;

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b bg-sidebar px-2 py-1">
        <div role="tablist" aria-label="GitHub" className="flex gap-0.5">
          {TABS.map((t) => (
            <button key={t.id} role="tab" aria-selected={tab === t.id} onClick={() => setTab(t.id)} className={cn("flex items-center gap-1.5 rounded-md px-2 py-1 text-[13px] outline-none focus-visible:ring-2 focus-visible:ring-ring", tab === t.id ? "bg-accent" : "text-muted-foreground hover:bg-accent/50")}>
              <t.icon className="size-3.5" />{t.label}
            </button>
          ))}
        </div>
        <span className="flex-1" />
        {tab !== "repos" && tab !== "notifications" && <RepoPicker />}
        <span className="text-muted-foreground text-xs" title={status.rate.map((r) => `${r.resource}: ${r.remaining}/${r.limit}`).join("\n")}>{status.user?.login}</span>
      </div>
      <div role="tabpanel" className="flex min-h-0 flex-1">
        {tab === "repos" && <ReposTab />}
        {tab === "issues" && <IssuesTab />}
        {tab === "pulls" && <PullsTab />}
        {tab === "notifications" && <NotificationsTab />}
        {tab === "actions" && <ActionsTab key={repo} />}
      </div>
    </div>
  );
}
