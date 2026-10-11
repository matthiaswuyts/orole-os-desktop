import { useEffect, useState } from "react";
import { Settings } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { Switch } from "@/components/ui/switch";
import { errText } from "@/features/agents/format";

/** The daemon reads the "agents" setting on every task start and mode change, so this takes effect at once. */
export function AgentSafetySettings() {
  const [allowed, setAllowed] = useState<boolean | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (!inDesktop) return;
    let live = true;
    Settings.all().then(
      (all) => { if (live) setAllowed(!(all.agents as { yoloDisabled?: boolean } | undefined)?.yoloDisabled); },
      (failure: unknown) => { if (live) setError(errText(failure)); },
    );
    return () => { live = false; };
  }, []);
  async function change(next: boolean) {
    setError(null);
    try { await Settings.set("agents", { yoloDisabled: !next }); setAllowed(next); }
    catch (failure) { setError(errText(failure)); }
  }
  return <div className="flex flex-col gap-1">
    <div className="flex items-center justify-between gap-6 px-3 py-2.5">
      <div className="min-w-0">
        <label htmlFor="allow-yolo" className="block">Allow YOLO mode</label>
        <p className="text-xs text-muted-foreground">YOLO runs a task without ever asking. Turning this off blocks starting or switching to it; tasks already in YOLO must be switched before they continue.</p>
      </div>
      <Switch id="allow-yolo" checked={allowed ?? true} disabled={!inDesktop || allowed === null} onCheckedChange={(checked) => void change(checked)} aria-label="Allow YOLO mode" />
    </div>
    {error && <p role="alert" className="px-3 text-xs text-destructive-foreground">{error}</p>}
  </div>;
}
