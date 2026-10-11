import { useEffect, useState } from "react";
import { Settings } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { Switch } from "@/components/ui/switch";
import { errText } from "@/features/agents/format";

type AgentsSetting = { yoloDisabled?: boolean; aiTitlesDisabled?: boolean };

/**
 * The daemon reads the "agents" setting on every task start, mode change and title request, so
 * switches take effect at once. The value is one JSON object shared by every switch here: each save
 * writes the whole object back so it never drops a key another switch (or a newer daemon) owns.
 */
export function AgentSafetySettings() {
  const [setting, setSetting] = useState<AgentsSetting | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (!inDesktop) return;
    let live = true;
    Settings.all().then(
      (all) => { if (live) setSetting((all.agents as AgentsSetting | undefined) ?? {}); },
      (failure: unknown) => { if (live) setError(errText(failure)); },
    );
    return () => { live = false; };
  }, []);
  async function change(patch: AgentsSetting) {
    if (!setting) return;
    setError(null);
    const next = { ...setting, ...patch };
    try { await Settings.set("agents", next); setSetting(next); }
    catch (failure) { setError(errText(failure)); }
  }
  const disabled = !inDesktop || setting === null;
  return <div className="flex flex-col gap-1">
    <div className="flex items-center justify-between gap-6 px-3 py-2.5">
      <div className="min-w-0">
        <label htmlFor="allow-yolo" className="block">Allow Yolo mode</label>
        <p className="text-xs text-muted-foreground">Yolo runs a task without ever asking. Turning this off blocks starting or switching to it; tasks already in Yolo must be switched before they continue.</p>
      </div>
      <Switch id="allow-yolo" checked={!setting?.yoloDisabled} disabled={disabled} onCheckedChange={(checked) => void change({ yoloDisabled: !checked })} aria-label="Allow Yolo mode" />
    </div>
    <div className="flex items-center justify-between gap-6 px-3 py-2.5">
      <div className="min-w-0">
        <label htmlFor="ai-task-titles" className="block">Name tasks with AI</label>
        <p className="text-xs text-muted-foreground">Uses your Claude or ChatGPT account for one short extra request per new task. Oh My Pi names its own tasks.</p>
      </div>
      <Switch id="ai-task-titles" checked={!setting?.aiTitlesDisabled} disabled={disabled} onCheckedChange={(checked) => void change({ aiTitlesDisabled: !checked })} aria-label="Name tasks with AI" />
    </div>
    {error && <p role="alert" className="px-3 text-xs text-destructive-foreground">{error}</p>}
  </div>;
}
