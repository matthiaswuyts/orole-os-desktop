import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { events } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Spinner } from "@/components/ui/spinner";

export const errorText = (error: unknown) => error instanceof Error ? error.message : String(error);

/** Discards responses from an older selection, and reloads when the daemon reconnects. */
export function useRemote<T>(load: () => Promise<T>, key: string, enabled = true) {
  const latest = useRef(load);
  latest.current = load;
  const [revision, setRevision] = useState(0);
  const [state, setState] = useState<{ key: string; data: T | null; error: string | null; loading: boolean }>({ key, data: null, error: null, loading: inDesktop && enabled });
  const reload = useCallback(() => setRevision((value) => value + 1), []);
  useEffect(() => {
    if (!inDesktop) return;
    const off = [events.mcpStatus.on(reload), events.mcpAuthDone.on(reload), events.daemonState.on(reload)];
    return () => off.forEach((unsubscribe) => unsubscribe());
  }, [reload]);
  useEffect(() => {
    if (!inDesktop || !enabled) { setState({ key, data: null, error: null, loading: false }); return; }
    let active = true;
    setState({ key, data: null, error: null, loading: true });
    latest.current().then(
      (data) => { if (active) setState({ key, data, error: null, loading: false }); },
      (error: unknown) => { if (active) setState({ key, data: null, error: errorText(error), loading: false }); },
    );
    return () => { active = false; };
  }, [key, enabled, revision]);
  if (!inDesktop || !enabled || state.key !== key) return { data: null, error: null, loading: inDesktop && enabled, reload };
  return { data: state.data, error: state.error, loading: state.loading, reload };
}

export function Failure({ error, retry }: { error: string | null; retry?: () => void }) {
  if (!error) return null;
  return <Alert variant="error"><AlertTitle>Request could not complete</AlertTitle><AlertDescription className="break-words">{error}</AlertDescription>{retry && <Button size="sm" variant="outline" onClick={retry}>Try again</Button>}</Alert>;
}

export function Loading({ children }: { children: ReactNode }) {
  return <div role="status" className="flex items-center gap-2 p-4 text-muted-foreground"><Spinner />{children}</div>;
}

export function NoResults({ title, children }: { title: string; children?: ReactNode }) {
  return <Empty><EmptyHeader><EmptyTitle>{title}</EmptyTitle>{children && <EmptyDescription>{children}</EmptyDescription>}</EmptyHeader></Empty>;
}

export function JsonData({ value }: { value: unknown }) {
  return <pre className="selectable max-h-80 overflow-auto whitespace-pre-wrap break-words rounded-md bg-muted p-3 text-xs">{JSON.stringify(value, null, 2)}</pre>;
}
