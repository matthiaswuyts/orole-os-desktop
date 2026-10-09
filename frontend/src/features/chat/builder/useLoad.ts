import { useCallback, useEffect, useRef, useState } from "react";
import { errText } from "@/features/projects/dialogs";

export interface Loaded<T> {
  data: T | null;
  error: string | null;
  loading: boolean;
  reload: () => void;
}

/**
 * Runs `loader` whenever `key` changes (and on `reload`), ignoring answers that arrive after a newer
 * request started or after unmount. The previous data stays visible while a reload is in flight.
 */
export function useLoad<T>(loader: () => Promise<T>, key: string, enabled = true): Loaded<T> {
  const [state, setState] = useState<{ data: T | null; error: string | null; loading: boolean }>({ data: null, error: null, loading: enabled });
  const latest = useRef(loader);
  useEffect(() => { latest.current = loader; });
  const [tick, setTick] = useState(0);
  const reload = useCallback(() => setTick((t) => t + 1), []);

  useEffect(() => {
    if (!enabled) { setState({ data: null, error: null, loading: false }); return; }
    let current = true;
    setState((s) => ({ ...s, loading: true, error: null }));
    latest.current().then(
      (data) => { if (current) setState({ data, error: null, loading: false }); },
      (cause: unknown) => { if (current) setState((s) => ({ data: s.data, error: errText(cause), loading: false })); },
    );
    return () => { current = false; };
  }, [key, tick, enabled]);

  return { ...state, reload };
}
