import { useCallback, useEffect, useRef, useState } from "react";
import { inDesktop } from "@/lib/backend";
import { errorText } from "./api";

/** Mounted surfaces poll only while the window is visible; stale responses never cross scopes. */
export function useAutomationResource<T>(load: () => Promise<T>, enabled = true) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const refreshRef = useRef<() => Promise<void>>(async () => {});
  useEffect(() => {
    let active = true;
    let pending = false;
    setData(null);
    setError(null);
    setLoading(true);
    const refresh = async () => {
      if (!active || pending || !enabled || !inDesktop || document.hidden) return;
      pending = true;
      try {
        const next = await load();
        if (active) { setData(next); setError(null); }
      } catch (e) {
        if (active) setError(errorText(e));
      } finally {
        pending = false;
        if (active) setLoading(false);
      }
    };
    refreshRef.current = refresh;
    void refresh();
    const timer = window.setInterval(() => void refresh(), 5000);
    const visible = () => { if (!document.hidden) void refresh(); };
    document.addEventListener("visibilitychange", visible);
    return () => {
      active = false;
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", visible);
    };
  }, [load, enabled]);
  const refresh = useCallback(() => refreshRef.current(), []);
  return { data, error, loading, refresh };
}
