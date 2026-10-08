import { useCallback, useEffect, useState, type ReactNode } from "react";
import { Copy, RefreshCw } from "lucide-react";
import { System, type AutostartStatus, type Report } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { Switch } from "@/components/ui/switch";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";

function Row({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-6 px-3 py-2.5">
      <div className="min-w-0">
        <div>{label}</div>
        {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
      </div>
      <div className="min-w-0 shrink-0">{children}</div>
    </div>
  );
}

/** Start-at-logon toggle plus the support snapshot from the desktop process (Settings > Diagnostics). */
export function AutostartPanel() {
  const [auto, setAuto] = useState<AutostartStatus | null>(null);
  const [report, setReport] = useState<Report | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setBusy(true);
    try {
      const [a, r] = await Promise.all([System.autostartStatus(), System.diagnostics()]);
      setAuto(a);
      setReport(r);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }, []);

  useEffect(() => {
    if (inDesktop) void load();
  }, [load]);

  if (!inDesktop) {
    return (
      <section className="mb-5">
        <h2 className="mb-2 text-sm font-medium">Start at login and support info</h2>
        <p className="rounded-lg border bg-card p-3 text-muted-foreground">Needs the desktop app: this browser preview has no daemon or Task Scheduler behind it.</p>
      </section>
    );
  }

  const toggle = async (on: boolean) => {
    setBusy(true);
    try {
      setAuto(on ? await System.enableAutostart() : await System.disableAutostart());
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const copy = () => {
    if (report) void navigator.clipboard.writeText(JSON.stringify(report, null, 2));
  };

  return (
    <section className="mb-5">
      <h2 className="text-sm font-medium">Start at login and support info</h2>
      <p className="mb-2 text-muted-foreground">The daemon starts when you sign in via a per-user scheduled task. No administrator rights, no Windows service.</p>
      <div className="divide-y rounded-lg border bg-card">
        {error && <p role="alert" className="px-3 py-2 text-destructive-foreground">{error}</p>}
        <Row label="Start daemon at login" hint={auto ? `${auto.mechanism}${auto.supported ? "" : " (unsupported)"}` : undefined}>
          <span className="flex items-center gap-2">
            {busy && <Spinner className="size-3.5" />}
            <Switch aria-label="Start daemon at login" checked={auto?.enabled ?? false} disabled={busy || !auto?.supported} onCheckedChange={(v) => void toggle(v)} />
          </span>
        </Row>
        {report && (
          <>
            <Row label="Credential store"><span className="font-mono" title={report.credentialStore.detail}>{report.credentialStore.ok ? "ok" : `failed: ${report.credentialStore.detail}`}</span></Row>
            <Row label="WebView2 runtime"><span className="font-mono">{report.webview2}</span></Row>
            <Row label="Database schema"><span className="font-mono">{report.dbSchemaVersion >= 0 ? `v${report.dbSchemaVersion} (${report.dbSource})` : `unknown (${report.dbSource})`}</span></Row>
            <Row label="Data directory"><span className="block max-w-sm truncate font-mono" title={report.dataDir}>{report.dataDir}</span></Row>
            <Row label="IPC pipe"><span className="block max-w-sm truncate font-mono" title={report.endpoint}>{report.endpoint}</span></Row>
            <div className="px-3 py-2.5">
              <div className="mb-1 text-xs text-muted-foreground">Last log lines ({report.logPath})</div>
              <pre className="max-h-40 overflow-auto rounded-md bg-muted/50 p-2 font-mono text-xs">{report.logTail.length ? report.logTail.join("\n") : "no log lines yet"}</pre>
            </div>
          </>
        )}
        <div className="flex gap-2 p-3">
          <Button size="xs" variant="outline" disabled={busy} onClick={() => void load()}><RefreshCw />Refresh</Button>
          <Button size="xs" variant="outline" disabled={!report} onClick={copy}><Copy />Copy diagnostics JSON</Button>
        </div>
      </div>
    </section>
  );
}
