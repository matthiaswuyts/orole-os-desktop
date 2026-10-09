import { useEffect } from "react";
import { inDesktop } from "@/lib/backend";
import { useDaemon } from "@/stores/daemon";
import { useHarnessOptionsCache } from "@/stores/harnessOptions";

/** Reuse machine discovery immediately; stale results refresh without clearing choices. */
export function useHarnessOptions(machine: string) {
  const link = useDaemon((state) => state.link);
  const epoch = useDaemon((state) => state.connectEpoch);
  const current = useHarnessOptionsCache((state) => state.machines[machine]);
  useEffect(() => {
    useHarnessOptionsCache.getState().load(machine, link, epoch);
  }, [machine, link, epoch]);
  return { options: current?.options, error: current?.error, loading: inDesktop && !current?.options && !current?.error, offline: link === "offline" };
}
