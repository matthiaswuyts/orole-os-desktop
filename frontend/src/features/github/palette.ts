import { createElement } from "react";
import { CircleDot, GitPullRequest } from "lucide-react";
import { GitHub } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { registerProvider, type Command } from "@/lib/commands";
import { useUI } from "@/stores/ui";
import { useGitHub } from "./store";

/** Server-side issue / pull request search in the command palette (queries GitHub, not a local list). */
export function githubQuery(q: string): string {
  // Scope to things the user is involved in unless they scoped it themselves.
  const scoped = /\b(repo|user|org|involves|author|assignee|mentions):/.test(q);
  return `${q} ${scoped ? "" : "involves:@me "}archived:false`.trim();
}

const unregisterIssues = registerProvider({
  id: "github",
  label: "GitHub",
  async search(query, signal): Promise<Command[]> {
    if (!inDesktop || query.length < 2 || signal.aborted) return [];
    const st = await GitHub.status();
    if (signal.aborted || !st.connected) return [];
    const res = await GitHub.search(githubQuery(query), 12);
    if (signal.aborted) return [];
    return res.items.map((i) => {
      const pr = !!i.pull_request;
      return {
        id: `github.${i.repo}#${i.number}`,
        title: `${i.title}`,
        category: `${i.repo} #${i.number}${pr ? " · pull request" : ""}`,
        icon: createElement(pr ? GitPullRequest : CircleDot),
        run: () => { useGitHub.getState().open({ kind: pr ? "pr" : "issue", repo: i.repo, number: i.number }); useUI.getState().setView("github"); },
      };
    });
  },
});

const unregisterRepos = registerProvider({
  id: "repositories",
  label: "Repositories",
  async search(query, signal): Promise<Command[]> {
    if (!inDesktop || query.length < 2 || signal.aborted) return [];
    const status = await GitHub.status();
    if (signal.aborted || !status.connected) return [];
    const repos = await GitHub.repos(query, 20);
    if (signal.aborted) return [];
    return repos.map((repo) => ({
      id: `github.repo.${repo.full_name}`,
      title: repo.full_name,
      category: `GitHub repository${repo.private ? " · private" : ""}`,
      run: () => {
        const state = useGitHub.getState();
        state.clearPending(); state.setRepo(repo.full_name); state.setTab("issues");
        useUI.getState().setView("github");
      },
    }));
  },
});

if (import.meta.hot) import.meta.hot.dispose(() => { unregisterIssues(); unregisterRepos(); });
