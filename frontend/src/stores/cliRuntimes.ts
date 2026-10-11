import { useEffect } from "react";
import { create } from "zustand";
import { CLIRuntimes, type CliruntimeStatus } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { onFocusRefresh } from "@/lib/focus";
import { readSnapshot, writeSnapshot } from "@/lib/snapshotCache";

const CACHE_KEY = "cli-runtimes";

/** What is running for a provider: a download, a sign-in or a sign-out. */
export type CLIBusy = "install" | "login" | "logout";

interface State {
  statuses: CliruntimeStatus[];
  ready: boolean;
  loading: boolean;
  error: string | null;
  busy: Record<string, CLIBusy>;
  /** The last failed action per provider, to show next to its row. */
  problems: Record<string, string>;
  load: () => Promise<void>;
  install: (id: string) => Promise<void>;
  login: (id: string) => Promise<void>;
  logout: (id: string) => Promise<void>;
}

/** Daemon errors carry a machine prefix ("internal: ..."); the user needs the sentence. */
const describe = (cause: unknown) => (cause instanceof Error ? cause.message : String(cause)).replace(/^(bad_request|not_found|conflict|canceled|timeout|internal|denied): /, "");

/**
 * The Claude, ChatGPT (Codex) and Grok CLIs: installed version, whether a newer one exists and who is signed in.
 * The daemon owns this; the store is a disposable snapshot that is refreshed after every action and when the window
 * regains focus (a sign-in finishes in the browser).
 */
export const useCLIRuntimes = create<State>((set, get) => {
  // Last known list: painted at once on open and replaced by the daemon's answer.
  const cached = inDesktop ? readSnapshot<CliruntimeStatus[]>(CACHE_KEY) : undefined;
  const replace = (status: CliruntimeStatus) => {
    set((s) => ({ statuses: s.statuses.map((x) => (x.id === status.id ? status : x)) }));
    writeSnapshot(CACHE_KEY, get().statuses);
  };
  const act = async (id: string, kind: CLIBusy, run: () => Promise<CliruntimeStatus>) => {
    if (get().busy[id]) return;
    set((s) => ({ busy: { ...s.busy, [id]: kind }, problems: { ...s.problems, [id]: "" } }));
    try {
      replace(await run());
    } catch (cause) {
      set((s) => ({ problems: { ...s.problems, [id]: describe(cause) } }));
      void get().load(); // the action may have partly happened (a sign-in the user finished anyway)
    } finally {
      set((s) => {
        const { [id]: _done, ...busy } = s.busy;
        return { busy };
      });
    }
  };
  return {
    statuses: cached ?? [], ready: cached !== undefined, loading: false, error: null, busy: {}, problems: {},
    async load() {
      if (!inDesktop || get().loading) return;
      set({ loading: true });
      try {
        const statuses = (await CLIRuntimes.list()) ?? [];
        set({ statuses, ready: true, error: null });
        writeSnapshot(CACHE_KEY, statuses);
      } catch (cause) {
        set({ error: describe(cause), ready: true });
      } finally {
        set({ loading: false });
      }
    },
    install: (id) => act(id, "install", () => CLIRuntimes.install(id)),
    login: (id) => act(id, "login", () => CLIRuntimes.login(id)),
    logout: (id) => act(id, "logout", () => CLIRuntimes.logout(id)),
  };
});

/** Loads the CLI statuses while a view is mounted and refreshes them when the window is focused again. */
export function useCLIRuntimesSync() {
  useEffect(() => {
    void useCLIRuntimes.getState().load();
    const refresh = () => void useCLIRuntimes.getState().load();
    return onFocusRefresh(refresh);
  }, []);
}
