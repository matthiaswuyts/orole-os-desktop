import { useCallback, useEffect, useState } from "react";
import { Sandbox, events } from "@/mygo";
import { errorText, type Availability, type ComputerStatus, type Handoff, type RuntimeInfo, type SetupInfo } from "./computerState";

/**
 * Availability, the bot's computer status and handoff request, and the async setup and download state, kept current
 * by the sandbox:* events. Setup (enable) and download (install) return at once; their results arrive as events.
 */
export function useComputer(botId: string) {
  const [availability, setAvailability] = useState<Availability | null>(null);
  const [status, setStatus] = useState<ComputerStatus | null>(null);
  const [handoff, setHandoff] = useState<Handoff | null>(null);
  const [runtime, setRuntime] = useState<RuntimeInfo | null>(null);
  const [setup, setSetup] = useState<SetupInfo | null>(null);
  const [error, setError] = useState<string | null>(null);
  const refresh = useCallback(async () => {
    try {
      const [a, s] = await Promise.all([Sandbox.availability(), Sandbox.status(botId)]);
      setAvailability(a);
      setStatus(s);
      setError(null);
    } catch (e) {
      setError(errorText(e));
    }
    try {
      setHandoff((await Sandbox.handoff(botId)) ?? null);
    } catch {
      // the handoff banner is optional
    }
  }, [botId]);
  useEffect(() => {
    setAvailability(null);
    setStatus(null);
    setHandoff(null);
    const offs = [
      events.sandboxStatus.on((s) => { if (s.botId === botId) setStatus(s); }),
      events.sandboxHandoff.on((h) => { if (h.botId === botId) setHandoff(h); }),
      events.sandboxAvailability.on((a) => setAvailability(a)),
      events.sandboxRuntime.on((r) => setRuntime(r)),
      events.sandboxSetup.on((s) => setSetup(s)),
    ];
    void refresh();
    return () => offs.forEach((off) => off());
  }, [botId, refresh]);
  const enable = useCallback(async () => {
    try {
      setSetup((await Sandbox.enable()) ?? null);
      setError(null);
    } catch (e) {
      setError(errorText(e));
    }
  }, []);
  const install = useCallback(async () => {
    try {
      setRuntime((await Sandbox.install()) ?? null);
      setError(null);
    } catch (e) {
      setError(errorText(e));
    }
  }, []);
  return { availability, status, handoff, runtime, setup, error, refresh, enable, install };
}

/** How often a visible Computer tab renews its viewer lease (the daemon drops it after 150 s). */
export const VIEWER_REFRESH_MS = 45_000;

/**
 * Tells the daemon someone is watching the bot's computer while the tab is mounted and the window visible, so the
 * idle stop waits. The daemon expires the lease on its own if this window disappears.
 */
export function useViewerLease(botId: string) {
  useEffect(() => {
    const set = (watching: boolean) => void Sandbox.setViewer(botId, watching)?.catch(() => {});
    let timer: ReturnType<typeof setInterval> | undefined;
    const start = () => {
      set(true);
      clearInterval(timer);
      timer = setInterval(() => set(true), VIEWER_REFRESH_MS);
    };
    const stop = () => {
      clearInterval(timer);
      timer = undefined;
      set(false);
    };
    const onVisibility = () => { if (document.visibilityState === "hidden") stop(); else start(); };
    if (document.visibilityState !== "hidden") start();
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      document.removeEventListener("visibilitychange", onVisibility);
      stop();
    };
  }, [botId]);
}
