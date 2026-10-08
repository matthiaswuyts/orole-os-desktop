import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { NeedsDesktop } from "@/features/shared/States";
import { inDesktop } from "@/lib/backend";
import { SetupScreen } from "./SetupScreen";
import { agoIso } from "./shared";
import { errMsg, useGitHub } from "./store";

function Row({ k, children }: { k: string; children: React.ReactNode }) {
  return <div className="flex items-center justify-between gap-6 px-3 py-2.5"><span>{k}</span><span className="min-w-0 truncate text-right text-muted-foreground">{children}</span></div>;
}

/** Settings > GitHub: connection state, token source, rate limits, sign out. */
export function GitHubSettings() {
  const { status, error, load, logout, startSync } = useGitHub();
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  useEffect(() => { void load(); return startSync(); }, [load, startSync]);

  if (!inDesktop) return <NeedsDesktop what="GitHub settings" />;
  if (!status) return <p role="status" className="text-muted-foreground">{error ?? "Checking GitHub connection…"}</p>;
  if (!status.connected) return <SetupScreen />;
  return (
    <>
      <section className="mb-5">
        <h2 className="text-sm font-medium">GitHub</h2>
        <p className="mb-2 text-muted-foreground">Writes (issues, comments, clones) always ask for confirmation and are recorded in the audit log.</p>
        <div className="divide-y rounded-lg border bg-card">
          <Row k="Signed in as">{status.user?.login}</Row>
          <Row k="Token source">{status.source === "gh" ? "GitHub CLI (token kept in memory only)" : "Personal access token (Windows Credential Manager)"}</Row>
          {status.scopes && <Row k="Token scopes"><span className="font-mono text-xs">{status.scopes}</span></Row>}
          {status.rate.map((r) => <Row key={r.resource} k={`Rate limit: ${r.resource}`}>{r.remaining} / {r.limit}, resets {agoIso(r.resetAt) === "never" ? "" : new Date(r.resetAt).toLocaleTimeString()}</Row>)}
          <div className="flex items-center justify-between gap-6 px-3 py-2.5">
            <span>Disconnect<span className="block text-muted-foreground text-xs">Removes the stored token (if any) and forgets this connection. Your gh login is untouched.</span></span>
            <Button variant="outline" size="sm" disabled={busy} onClick={() => { setBusy(true); setErr(null); logout().catch((e) => setErr(errMsg(e))).finally(() => setBusy(false)); }}>Sign out</Button>
          </div>
        </div>
        {err && <p role="alert" className="mt-2 text-destructive-foreground">{err}</p>}
      </section>
    </>
  );
}
