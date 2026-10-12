import { useEffect } from "react";
import { create } from "zustand";
import { Automations, type Approval } from "@/mygo";
import { inDesktop } from "@/lib/backend";

const POLL_MS = 30_000;
const NONE: Approval[] = [];

interface ApprovalsState {
  items: Approval[];
  /** When the last successful check finished (ms); 0 before the first one. */
  checkedAt: number;
}

export const useApprovalsStore = create<ApprovalsState>(() => ({ items: NONE, checkedAt: 0 }));

let consumers = 0;
let timer: number | undefined;

function load() {
  Automations.approvals().then(
    (items) => useApprovalsStore.setState({ items: items?.length ? items : NONE, checkedAt: Date.now() }),
    (error: unknown) => console.warn("Could not load automation approvals", error),
  );
}

/**
 * Automation runs waiting for an approval. Every active consumer shares one poll (every 30 s while
 * at least one is mounted); an inactive consumer gets an empty list and does not keep it running.
 */
export function useAutomationApprovals(active = true): Approval[] {
  useEffect(() => {
    if (!active || !inDesktop) return;
    if (consumers++ === 0) {
      load();
      timer = window.setInterval(load, POLL_MS);
    }
    return () => {
      if (--consumers === 0) window.clearInterval(timer);
    };
  }, [active]);
  const items = useApprovalsStore((s) => s.items);
  return active ? items : NONE;
}
