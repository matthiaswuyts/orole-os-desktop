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
  latest.current = loader;
  const token = useRef(0);
  const [tick, setTick] = useState(0);
  const reload = useCallback(() => setTick((t) => t + 1), []);

  useEffect(() => {
    const mine = ++token.current;
    if (!enabled) { setState({ data: null, error: null, loading: false }); return; }
    setState((s) => ({ ...s, loading: true, error: null }));
    latest.current().then(
      (data) => { if (mine === token.current) setState({ data, error: null, loading: false }); },
      (cause: unknown) => { if (mine === token.current) setState((s) => ({ data: s.data, error: errText(cause), loading: false })); },
    );
    return () => { token.current++; };
  }, [key, tick, enabled]);

  return { ...state, reload };
}
