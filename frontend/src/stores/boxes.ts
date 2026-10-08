import { create } from "zustand";
import { events, Machines, MCP, type Machine, type MachineHealth, type ServerState } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { useDaemon } from "@/stores/daemon";

export const BOX_STALE_MS = 45_000;
export interface BoxObservation { health: MachineHealth | null; error: string; attemptedAt: number }
interface BoxesState {
  selectedId: string;
  machines: Machine[];
  observations: Record<string, BoxObservation>;
  loaded: boolean;
  error: string;
  now: number;
  mcp: ServerState[] | null;
  mcpError: string;
  refresh: () => Promise<void>;
}
const message = (error: unknown) => error instanceof Error ? error.message : String(error);
let pending: Promise<void> | undefined;
let consumers = 0;
const sameTarget = (a: Machine, b: Machine) => a.address === b.address && a.fingerprint === b.fingerprint && a.username === b.username && a.transport === b.transport && a.backend === b.backend && a.allowCommands === b.allowCommands && a.allowWrite === b.allowWrite && JSON.stringify(a.roots) === JSON.stringify(b.roots);

export const useBoxes = create<BoxesState>((set, get) => ({
  machines: [], observations: {}, loaded: false, error: "", now: Date.now(), mcp: null, mcpError: "", selectedId: "local",
  refresh() {
    if (!inDesktop || document.hidden || useDaemon.getState().link !== "connected") return Promise.resolve();
    if (pending) return pending;
    pending = (async () => {
      set({ now: Date.now() });
      const mcp = MCP.list().then((servers) => set({ mcp: servers, mcpError: "" }), (error: unknown) => set({ mcpError: message(error) }));
      try {
        const machines = await Machines.list();
        const old = get();
        const observations: Record<string, BoxObservation> = {};
        for (const machine of machines) {
          const previous = old.machines.find((item) => item.id === machine.id);
          if (previous && sameTarget(previous, machine) && old.observations[machine.id]) observations[machine.id] = old.observations[machine.id];
        }
        set({ machines, observations, loaded: true, error: "" });
        let next = 0;
        await Promise.all(Array.from({ length: Math.min(4, machines.length) }, async () => {
          while (next < machines.length && consumers > 0 && !document.hidden && useDaemon.getState().link === "connected") {
            const machine = machines[next++];
            const cached = get().observations[machine.id];
            if (cached && Date.now() - cached.attemptedAt < 15_000) continue;
            let observation: BoxObservation;
            try { observation = { health: await Machines.health(machine.id), error: "", attemptedAt: Date.now() }; }
            catch (error) { observation = { health: null, error: message(error), attemptedAt: Date.now() }; }
            set((state) => ({ observations: { ...state.observations, [machine.id]: observation }, now: Date.now() }));
          }
        }));
      } catch (error) { set({ error: message(error) }); }
      await mcp;
    })().finally(() => { pending = undefined; });
    return pending;
  },
}));

export function boxStatus(observation: BoxObservation | undefined, now: number): "unknown" | "stale" | "error" | "offline" | "online" {
  if (!observation) return "unknown";
  if (now - observation.attemptedAt > BOX_STALE_MS) return "stale";
  if (observation.error) return "error";
  if (!observation.health) return "unknown";
  return observation.health.reachable ? "online" : "offline";
}

/** Demand-owned, single-flight and visibility-aware; at most four fixed probes at once. */
export function startBoxesPolling(): () => void {
  if (!inDesktop) return () => {};
  consumers++;
  const refresh = () => { void useBoxes.getState().refresh(); };
  refresh();
  const timer = setInterval(refresh, 15_000);
  const visible = () => { if (!document.hidden) refresh(); };
  document.addEventListener("visibilitychange", visible);
  const offDaemon = useDaemon.subscribe((state, previous) => { if (state.link === "connected" && previous.link !== "connected") refresh(); });
  const offMcp = events.mcpStatus.on((server) => useBoxes.setState((state) => ({ mcp: state.mcp?.map((item) => item.id === server.id ? server : item) ?? null })));
  return () => { consumers--; clearInterval(timer); document.removeEventListener("visibilitychange", visible); offDaemon(); offMcp(); };
}
