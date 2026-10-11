import { create } from "zustand";
import { GitHub, events, type GitHubStatus } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { onFocusRefresh } from "@/lib/focus";

export type GitHubTab = "repos" | "issues" | "pulls" | "notifications" | "actions";
export interface OpenItem { kind: "issue" | "pr"; repo: string; number: number }

const REPO_KEY = "orole.github.repo";
let subscribers = 0;
let stop: (() => void) | undefined;

export const errMsg = (e: unknown) => (e instanceof Error ? e.message : String(e)).replace(/^(bad_request|not_found|not_connected|denied|internal): /, "");

interface State {
  /** False in the browser preview: there is no daemon, so there is no GitHub data. */
  available: boolean;
  status: GitHubStatus | null;
  loading: boolean;
  error: string | null;
  tab: GitHubTab;
  /** Repository the Issues / Pull requests / Actions tabs operate on. */
  repo: string;
  /** Item to open once its tab mounts (set by the palette and widgets). */
  pending: OpenItem | null;
  /** Bumped when daemon data changed so tabs refetch. */
  rev: number;
  setTab: (t: GitHubTab) => void;
  setRepo: (r: string) => void;
  open: (item: OpenItem) => void;
  clearPending: () => void;
  load: () => Promise<void>;
  login: (source: "gh" | "pat", token: string) => Promise<void>;
  logout: () => Promise<void>;
  startSync: () => () => void;
}

export const useGitHub = create<State>((set, get) => ({
  available: inDesktop,
  status: null,
  loading: false,
  error: null,
  tab: "issues",
  repo: localStorage.getItem(REPO_KEY) ?? "",
  pending: null,
  rev: 0,
  setTab: (tab) => set({ tab }),
  setRepo: (repo) => { localStorage.setItem(REPO_KEY, repo); set({ repo }); },
  open: (item) => set({ tab: item.kind === "issue" ? "issues" : "pulls", repo: item.repo, pending: item }),
  clearPending: () => set({ pending: null }),
  load: async () => {
    if (!inDesktop) return;
    set({ loading: true });
    try {
      set({ status: await GitHub.status(), error: null, loading: false });
    } catch (e) {
      set({ error: errMsg(e), loading: false });
    }
  },
  login: async (source, token) => {
    const status = await GitHub.login(source, token);
    set({ status, error: null });
  },
  logout: async () => set({ status: await GitHub.logout() }),
  startSync: () => {
    if (!inDesktop) return () => {};
    // Several views mount this; one subscription serves them all so a daemon event bumps `rev` and loads once.
    if (++subscribers === 1) {
      const off = events.githubChanged.on(() => { set((s) => ({ rev: s.rev + 1 })); void get().load(); });
      const offFocus = onFocusRefresh(() => void get().load());
      stop = () => { off(); offFocus(); };
    }
    let stopped = false;
    return () => {
      if (stopped) return;
      stopped = true;
      if (--subscribers === 0) { stop?.(); stop = undefined; }
    };
  },
}));
