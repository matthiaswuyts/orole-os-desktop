import type { Project } from "@/mygo";

const VALID = /^[A-Za-z0-9._-]+\/[A-Za-z0-9._-]+$/;

/** Mirrors internal/github.RepoFromRemote: only github.com https/ssh remotes without port, query or fragment. */
export function repoFromRemote(remote: string): string {
  const raw = remote.startsWith("git@github.com:") ? `ssh://git@github.com/${remote.slice("git@github.com:".length)}` : remote;
  let url: URL;
  try { url = new URL(raw); } catch { return ""; }
  if (url.hostname.toLowerCase() !== "github.com" || url.port || url.search || url.hash || (url.protocol !== "https:" && url.protocol !== "ssh:")) return "";
  const repo = url.pathname.replace(/^\//, "").replace(/\.git$/, "");
  const [owner, name] = repo.split("/");
  if (!VALID.test(repo) || owner === "." || owner === ".." || name === "." || name === "..") return "";
  return repo.toLowerCase();
}

export interface IssueScope { id: string; name: string; repo: string }

/** One entry per GitHub-backed project; repositories shared by several projects keep the first project's name. */
export function issueScopes(projects: Project[]): IssueScope[] {
  const seen = new Set<string>();
  const out: IssueScope[] = [];
  for (const project of projects) {
    const repo = repoFromRemote(project.remoteUrl);
    if (!repo || seen.has(repo)) continue;
    seen.add(repo);
    out.push({ id: project.id, name: project.name, repo });
  }
  return out;
}
