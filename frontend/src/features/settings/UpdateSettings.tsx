import { Button } from "@/components/ui/button";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { useUpdates } from "@/stores/updates";
import { inDesktop } from "@/lib/backend";

export function UpdateSettings() {
  const { status, error, busy, check, install } = useUpdates();
  if (!inDesktop) return <p className="text-muted-foreground">Update checks require the installed desktop application.</p>;
  return (
    <section aria-label="Desktop updates" className="flex max-w-prose flex-col gap-4">
      <p>Installed version: <span className="font-mono">{status?.currentVersion || "Checking…"}</span></p>
      {status && !status.enabled ? (
        <Alert><AlertDescription>Signed updates are not configured or supported by this build. No update is advertised or downloaded. The maintainer must publish signed MyGo releases and include the trusted public key in the installed build.</AlertDescription></Alert>
      ) : (
        <>
          <p className="text-muted-foreground">{status?.available ? `Version ${status.version} is available.` : busy === "check" ? "Checking the signed release feed…" : status ? "No newer release was found at the last successful check." : "Update status has not loaded."}</p>
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" disabled={busy !== null} onClick={() => void check()}>Check for updates</Button>
            {status?.enabled && status.available && <Button disabled={busy !== null} onClick={() => void install()}>{busy === "install" ? "Updating…" : `Install ${status.version} and restart`}</Button>}
          </div>
          <p className="text-xs text-muted-foreground">Installation requires native confirmation. Save your work first: updating stops daemon-owned agents, terminals, and automations. The release is verified by MyGo before replacement.</p>
        </>
      )}
      {error && <Alert variant="error"><AlertDescription>{error}</AlertDescription></Alert>}
    </section>
  );
}
