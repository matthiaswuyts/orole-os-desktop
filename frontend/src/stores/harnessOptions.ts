import { create } from "zustand";
import { Agents, type LaunchOptions } from "@/mygo";
import { inDesktop } from "@/lib/backend";

const STALE_MS = 45_000;
interface Discovery {
  options?: LaunchOptions;
  error?: string;
  updatedAt: number;
  /** Daemon connection epoch the data was requested under; a reconnect makes it stale. */
  epoch: number;
  request?: symbol;
}
interface HarnessOptionsState {
  machines: Record<string, Discovery>;
  /** Fetches unless the cached result is fresh for this connection epoch or a request for it is already in flight. */
  load: (machine: string, link: string, epoch: number) => void;
}

/** Shared by Home, new-task dialogs and task composers; never persisted to disk. */
export const useHarnessOptionsCache = create<HarnessOptionsState>((set, get) => ({
  machines: {},
  load(machine, link, epoch) {
    if (!inDesktop || link === "offline") return;
    const cached = get().machines[machine];
    if (cached?.request && cached.epoch === epoch) return;
    if (cached?.options && cached.epoch === epoch && Date.now() - cached.updatedAt < STALE_MS) return;
    const request = Symbol(machine);
    set((state) => ({ machines: { ...state.machines, [machine]: { options: cached?.options, updatedAt: 0, epoch, request } } }));
    const complete = (result: Pick<Discovery, "options" | "error">) => {
      // A newer request (reconnect) supersedes this one; its late reply must not overwrite fresher data.
      if (get().machines[machine]?.request !== request) return;
      set((state) => ({ machines: { ...state.machines, [machine]: { options: cached?.options, ...result, updatedAt: result.error ? 0 : Date.now(), epoch } } }));
    };
    void Agents.options(machine).then(
      (options) => complete({ options }),
      (error: unknown) => complete({ error: error instanceof Error ? error.message : String(error) }),
    );
  },
}));
