import { useEffect, useState } from "react";
import { Policy, Settings, type Rule } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { Button } from "@/components/ui/button";
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { errText, useAction } from "@/features/agents/format";
import { isBotActor } from "@/features/mcp/usePolicyPending";
import type { PermissionMode } from "@/features/tasks/permissionModes";

export type AgentsSetting = {
  yoloDisabled?: boolean;
  aiTitlesDisabled?: boolean;
  /** The mode a task starts in when none was chosen; "" or absent means Manual. */
  defaultPermissionMode?: PermissionMode | "";
  botYolo?: boolean;
  botAutoApprove?: boolean;
};

const MODES: { value: PermissionMode; label: string }[] = [{ value: "manual", label: "Manual" }, { value: "auto", label: "Auto" }, { value: "yolo", label: "Yolo" }];

/**
 * The daemon reads the "agents" setting on every task start, mode change and title request, so
 * switches take effect at once. The value is one JSON object shared by every control here: each save
 * writes the whole object back so it never drops a key another control (or a newer daemon) owns.
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
  const yoloOff = !!setting?.yoloDisabled;
  const defaultMode: PermissionMode = setting?.defaultPermissionMode === "auto" || (setting?.defaultPermissionMode === "yolo" && !yoloOff) ? setting.defaultPermissionMode : "manual";
  return <div className="flex flex-col gap-1">
    <div className="flex items-center justify-between gap-6 px-3 py-2.5">
      <div className="min-w-0">
        <label htmlFor="allow-yolo" className="block">Allow Yolo mode</label>
        <p className="text-xs text-muted-foreground">Yolo runs a task without ever asking. Turning this off blocks starting or switching to it; tasks already in Yolo must be switched before they continue.</p>
      </div>
      <Switch id="allow-yolo" checked={!yoloOff} disabled={disabled} onCheckedChange={(checked) => void change({ yoloDisabled: !checked })} aria-label="Allow Yolo mode" />
    </div>
    <div className="flex items-center justify-between gap-6 px-3 py-2.5">
      <div className="min-w-0">
        <label htmlFor="ai-task-titles" className="block">Name tasks with AI</label>
        <p className="text-xs text-muted-foreground">Uses your Claude or ChatGPT account for one short extra request per new task. Oh My Pi names its own tasks.</p>
      </div>
      <Switch id="ai-task-titles" checked={!setting?.aiTitlesDisabled} disabled={disabled} onCheckedChange={(checked) => void change({ aiTitlesDisabled: !checked })} aria-label="Name tasks with AI" />
    </div>
    <div className="flex items-center justify-between gap-6 px-3 py-2.5">
      <div className="min-w-0">
        <span id="default-permission-mode-label" className="block">Default permission mode</span>
        <p className="text-xs text-muted-foreground">How a new task runs when nobody picked a mode: yours from Home and the ones bots start. Each task can still be switched.{yoloOff ? " Yolo is turned off above." : ""}</p>
      </div>
      <Select value={defaultMode} items={MODES} disabled={disabled} onValueChange={(mode: PermissionMode | null) => { if (mode) void change({ defaultPermissionMode: mode }); }}>
        <SelectTrigger aria-labelledby="default-permission-mode-label" className="w-40 shrink-0"><SelectValue /></SelectTrigger>
        <SelectPopup>
          {MODES.map((mode) => <SelectItem key={mode.value} value={mode.value} disabled={mode.value === "yolo" && yoloOff}>{mode.label}</SelectItem>)}
        </SelectPopup>
      </Select>
    </div>
    <div className="flex items-center justify-between gap-6 px-3 py-2.5">
      <div className="min-w-0">
        <label htmlFor="bot-yolo" className="block">Bots may use Yolo</label>
        <p className="text-xs text-muted-foreground">Lets a bot start a task in Yolo when you ask it to, or when Yolo is the default.{yoloOff ? " Needs Yolo mode allowed above." : ""}</p>
      </div>
      <Switch id="bot-yolo" checked={!!setting?.botYolo && !yoloOff} disabled={disabled || yoloOff} onCheckedChange={(checked) => void change({ botYolo: checked })} aria-label="Bots may use Yolo" />
    </div>
    <div className="flex items-center justify-between gap-6 px-3 py-2.5">
      <div className="min-w-0">
        <label htmlFor="bot-auto-approve" className="block">Bots start tasks without asking</label>
        <p className="text-xs text-muted-foreground">Off: every start and follow-up a bot sends waits for your approval in its chat, unless you allowed that bot always.</p>
      </div>
      <Switch id="bot-auto-approve" checked={!!setting?.botAutoApprove} disabled={disabled} onCheckedChange={(checked) => void change({ botAutoApprove: checked })} aria-label="Bots start tasks without asking" />
    </div>
    <RememberedApprovals />
    {error && <p role="alert" className="px-3 text-xs text-destructive-foreground">{error}</p>}
  </div>;
}

/** The "Always allow this bot" decisions made in chat: stored policy rules for bot actors on the task actions. */
function RememberedApprovals() {
  const [rules, setRules] = useState<Rule[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const removal = useAction();
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    if (!inDesktop) return;
    let live = true;
    Policy.rules().then(
      (all) => { if (live) { setRules((all ?? []).filter((rule) => isBotActor(rule.actor) && rule.action.startsWith("agent."))); setError(null); } },
      (failure: unknown) => { if (live) setError(errText(failure)); },
    );
    return () => { live = false; };
  }, [revision]);
  if (!inDesktop) return null;
  return <div className="flex flex-col gap-1 px-3 py-2.5">
    <span className="block">Remembered approvals</span>
    <p className="text-xs text-muted-foreground">Bots you allowed always from a chat. Removing one makes that bot ask again.</p>
    {error && <p role="alert" className="text-xs text-destructive-foreground">{error} <Button type="button" variant="link" size="xs" onClick={() => setRevision((value) => value + 1)}>Retry</Button></p>}
    {rules?.length === 0 && <p className="text-xs text-muted-foreground">None yet.</p>}
    {rules && rules.length > 0 && <ul className="mt-1 flex flex-col divide-y divide-border/60 rounded-lg border">
      {rules.map((rule) => <li key={rule.id} className="flex items-center justify-between gap-3 px-2.5 py-1.5 text-xs">
        <span className="min-w-0 flex-1 truncate"><span className="font-mono">{rule.actor}</span> · {rule.action} · {rule.resource}</span>
        <Button type="button" variant="outline" size="xs" disabled={removal.busy} onClick={() => void removal.run(async () => { await Policy.deleteRule(rule.id); setRules((list) => (list ?? []).filter((item) => item.id !== rule.id)); })}>Remove</Button>
      </li>)}
    </ul>}
    {removal.error && <p role="alert" className="text-xs text-destructive-foreground">{removal.error}</p>}
  </div>;
}
