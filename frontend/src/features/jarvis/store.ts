import { create } from "zustand";
import { errText } from "@/features/agents/format";
import { inDesktop } from "@/lib/backend";
import { events, Jarvis, type StatusView } from "@/mygo";

/** Phone setup status. Holds no secrets: those live only in component state while being entered. */
interface JarvisStore {
  status: StatusView | null;
  error: string | null;
  load: () => Promise<void>;
  apply: (v: StatusView) => void;
  startSync: () => () => void;
}

export const useJarvis = create<JarvisStore>((set) => ({
  status: null,
  error: null,
  load: async () => {
    if (!inDesktop) return;
    try {
      set({ status: await Jarvis.status(), error: null });
    } catch (failure) {
      set({ error: errText(failure) });
    }
  },
  apply: (status) => set({ status, error: null }),
  startSync: () => events.jarvisChanged.on((v) => set({ status: v })),
}));

/** True when calls can be placed: every setup step is done. */
export function phoneReady(s: StatusView | null): boolean {
  return !!s && s.hasTwilio && s.hasElevenLabs && s.hasRelaySecrets && s.relayConnected && !!s.settings.agentId && !!s.settings.ownerVerified;
}
