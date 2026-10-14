import { useEffect, useState, type ReactNode } from "react";
import { Chat, Policy, Settings, type BotTaskDefaults, type Rule } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { Button } from "@/components/ui/button";
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { errText, useAction } from "@/features/agents/format";
import { useHarnessOptions } from "@/features/agents/useHarnessOptions";
import {
  PERMISSION_LABELS, TASK_AUTO, TOOL_ACCESS, botHarnesses, botYoloBlocked, fitTaskChoice, permissionModeOf, taskChoices, toolAccessOf,
  type ToolAccess,
} from "@/features/chat/botTrust";
import { botRequestTitle, isBotActor } from "@/features/mcp/usePolicyPending";
import { SettingsSection } from "@/features/settings/SettingsSection";
import type { PermissionMode } from "@/features/tasks/permissionModes";

export type AgentsSetting = {
  yoloDisabled?: boolean;
  aiTitlesDisabled?: boolean;
  /** The mode a task starts in when none was chosen; "" or absent means Manual. */
  defaultPermissionMode?: PermissionMode | "";
  botYolo?: boolean;
  botAutoApprove?: boolean;
  /** How much bots may do through integrations and tools without asking; "" or absent means "ask". */
  botToolAccess?: ToolAccess | "";
  /** Code tasks bots start: empty harness/model/reasoning mean Auto, an empty permission mode the default mode. */
  botTasks?: BotTaskDefaults;
};

const MODES: { value: PermissionMode; label: string }[] = [{ value: "manual", label: "Manual" }, { value: "auto", label: "Auto" }, { value: "yolo", label: "Yolo" }];

/** A setting row with a select on the right; the select is named by the row's label. */
function SelectRow({ id, label, hint, children }: { id: string; label: string; hint?: ReactNode; children: ReactNode }) {
  return <div className="flex items-center justify-between gap-6 px-3 py-2.5">
    <div className="min-w-0">
      <span id={`${id}-label`} className="block">{label}</span>
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </div>
    {children}
  </div>;
}

/**
 * The daemon reads the "agents" setting on every task start, mode change, title request and bot tool call, so changes take
 * effect at once. The value is one JSON object shared by every control here: each save writes the whole object back so it
 * never drops a key another control (or a newer daemon) owns.
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
  const access = toolAccessOf(setting?.botToolAccess) || "ask";
  return <>
    <SettingsSection id="settings-task-permissions" title="Task permissions" description="Manual, Auto and Yolo are chosen per task; the default here applies when nobody picked one.">
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
      <SelectRow id="default-permission-mode" label="Default permission mode" hint={`How a new task runs when nobody picked a mode, including the ones bots start unless Bot code tasks says otherwise. Each task can still be switched.${yoloOff ? " Yolo is turned off above." : ""}`}>
        <Select value={defaultMode} items={MODES} disabled={disabled} onValueChange={(mode: PermissionMode | null) => { if (mode) void change({ defaultPermissionMode: mode }); }}>
          <SelectTrigger aria-labelledby="default-permission-mode-label" className="w-40 shrink-0"><SelectValue /></SelectTrigger>
          <SelectPopup>
            {MODES.map((mode) => <SelectItem key={mode.value} value={mode.value} disabled={mode.value === "yolo" && yoloOff}>{mode.label}</SelectItem>)}
          </SelectPopup>
        </Select>
      </SelectRow>
    </SettingsSection>
    <SettingsSection id="settings-bot-tools" title="Bot tools" description="How much bots may do through integrations and tools before they ask you. A bot's own settings can choose differently.">
      <SelectRow id="bot-tool-access" label="Without asking, bots may" hint={<>{TOOL_ACCESS.find((level) => level.value === access)!.hint} An integration tool you set to Always ask or Block still asks or stays blocked.</>}>
        <Select value={access} items={TOOL_ACCESS} disabled={disabled} onValueChange={(level: ToolAccess | null) => { if (level) void change({ botToolAccess: level }); }}>
          <SelectTrigger aria-labelledby="bot-tool-access-label" className="w-64 shrink-0"><SelectValue /></SelectTrigger>
          <SelectPopup alignItemWithTrigger={false} className="w-80">
            {TOOL_ACCESS.map((level) => <SelectItem key={level.value} value={level.value} className="items-start py-1.5">
              <span className="flex min-w-0 flex-col gap-0.5">
                <span className="font-medium">{level.label}</span>
                <span className="text-xs text-muted-foreground">{level.hint}</span>
              </span>
            </SelectItem>)}
          </SelectPopup>
        </Select>
      </SelectRow>
    </SettingsSection>
    <SettingsSection id="settings-bot-tasks" title="Bot code tasks" description="How the code tasks your bots start run. Auto lets the bot choose for each task. A bot's own settings can choose differently.">
      <BotTaskDefaultsRows setting={setting} disabled={disabled} defaultMode={defaultMode} change={change} />
      <div className="flex items-center justify-between gap-6 px-3 py-2.5">
        <div className="min-w-0">
          <label htmlFor="bot-yolo" className="block">Bots may use Yolo</label>
          <p className="text-xs text-muted-foreground">Lets a bot start a task in Yolo when its permission mode is Yolo.{yoloOff ? " Needs Yolo mode allowed above." : ""}</p>
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
    </SettingsSection>
    {error && <p role="alert" className="-mt-3 mb-5 px-3 text-xs text-destructive-foreground">{error}</p>}
  </>;
}

/** Harness, model, effort and permission mode for the code tasks bots start. */
function BotTaskDefaultsRows({ setting, disabled, defaultMode, change }: { setting: AgentsSetting | null; disabled: boolean; defaultMode: PermissionMode; change: (patch: AgentsSetting) => Promise<void> }) {
  const source = useHarnessOptions("local");
  const harnesses = botHarnesses(source.options?.harnesses);
  const tasks = setting?.botTasks ?? {};
  const harness = tasks.harness || TASK_AUTO;
  const model = tasks.model || TASK_AUTO;
  const reasoning = tasks.reasoning || TASK_AUTO;
  const mode = permissionModeOf(tasks.permissionMode);
  const { models, levels } = taskChoices(harnesses, harness, model);
  const yoloBlocked = botYoloBlocked(setting);
  const save = (patch: BotTaskDefaults) => void change({ botTasks: { ...tasks, ...patch } });

  const installed = harnesses.filter((h) => h.available);
  const harnessItems = [{ value: TASK_AUTO, label: "Auto — the bot chooses" }, ...installed.map((h) => ({ value: h.id, label: h.name || h.id }))];
  if (!harnessItems.some((item) => item.value === harness)) harnessItems.push({ value: harness, label: `${harnesses.find((h) => h.id === harness)?.name || harness} (not installed)` });
  const modelItems = [{ value: TASK_AUTO, label: harness === TASK_AUTO ? "Auto" : "Auto — the bot chooses" }, ...models.map((m) => ({ value: m.id, label: m.name || m.id }))];
  if (!modelItems.some((item) => item.value === model)) modelItems.push({ value: model, label: model });
  const levelItems = [{ value: TASK_AUTO, label: harness === TASK_AUTO ? "Auto" : "Auto — the bot chooses" }, ...levels.map((level) => ({ value: level, label: level }))];
  if (!levelItems.some((item) => item.value === reasoning)) levelItems.push({ value: reasoning, label: reasoning });
  const modeItems = [{ value: "", label: `Same as my default (${PERMISSION_LABELS[defaultMode]})` }, ...MODES];
  const discovering = source.loading ? " Looking for installed agents…" : source.error ? ` Could not list installed agents: ${source.error}` : "";

  return <>
    <SelectRow id="bot-task-harness" label="Agent" hint={`Which coding agent runs the task. Auto lets the bot pick any installed one.${discovering}`}>
      <Select value={harness} items={harnessItems} disabled={disabled} onValueChange={(next: string | null) => { if (next) save({ harness: next, ...fitTaskChoice(harnesses, { harness: next, model, reasoning }) }); }}>
        <SelectTrigger aria-labelledby="bot-task-harness-label" className="w-56 shrink-0"><SelectValue /></SelectTrigger>
        <SelectPopup>{harnessItems.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}</SelectPopup>
      </Select>
    </SelectRow>
    <SelectRow id="bot-task-model" label="Model" hint={harness === TASK_AUTO ? "Pick an agent first to fix its model. With Auto the bot chooses, or the agent's default is used." : "Auto lets the bot choose, or uses the agent's default."}>
      <Select value={model} items={modelItems} disabled={disabled || harness === TASK_AUTO} onValueChange={(next: string | null) => { if (next) save({ model: next, reasoning: fitTaskChoice(harnesses, { harness, model: next, reasoning }).reasoning }); }}>
        <SelectTrigger aria-labelledby="bot-task-model-label" className="w-56 shrink-0"><SelectValue /></SelectTrigger>
        <SelectPopup>{modelItems.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}</SelectPopup>
      </Select>
    </SelectRow>
    <SelectRow id="bot-task-reasoning" label="Effort" hint={levels.length === 0 && harness !== TASK_AUTO ? "This agent reports no effort levels." : "How hard the agent thinks. Auto lets the bot choose, or uses the agent's default."}>
      <Select value={reasoning} items={levelItems} disabled={disabled || (levels.length === 0 && reasoning === TASK_AUTO)} onValueChange={(next: string | null) => { if (next) save({ reasoning: next }); }}>
        <SelectTrigger aria-labelledby="bot-task-reasoning-label" className="w-56 shrink-0 capitalize"><SelectValue /></SelectTrigger>
        <SelectPopup>{levelItems.map((item) => <SelectItem key={item.value} value={item.value} className="capitalize">{item.label}</SelectItem>)}</SelectPopup>
      </Select>
    </SelectRow>
    <SelectRow id="bot-task-mode" label="Permission mode" hint={mode === "yolo" && yoloBlocked ? `Bots cannot start tasks in Yolo right now, so their starts are refused. ${yoloBlocked}` : "Bots never choose this themselves."}>
      <Select value={mode} items={modeItems} disabled={disabled} onValueChange={(next: PermissionMode | "" | null) => { if (next !== null) save({ permissionMode: next }); }}>
        <SelectTrigger aria-labelledby="bot-task-mode-label" className="w-56 shrink-0"><SelectValue /></SelectTrigger>
        <SelectPopup alignItemWithTrigger={false} className="w-72">
          {modeItems.map((item) => {
            const blocked = item.value === "yolo" ? yoloBlocked : "";
            return <SelectItem key={item.value} value={item.value} disabled={!!blocked} className="items-start py-1.5">
              <span className="flex min-w-0 flex-col gap-0.5">
                <span>{item.label}</span>
                {blocked && <span className="text-xs text-muted-foreground">{blocked}</span>}
              </span>
            </SelectItem>;
          })}
        </SelectPopup>
      </Select>
    </SelectRow>
  </>;
}

const DECISIONS: Record<string, string> = { allow: "Allowed always", ask: "Always asks", deny: "Blocked" };

/** Stored policy rules for bot actors: the "Always" decisions made in chat and any other rule that names a bot. */
function RememberedApprovals() {
  const [rules, setRules] = useState<Rule[] | null>(null);
  const [names, setNames] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);
  const removal = useAction();
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    if (!inDesktop) return;
    let live = true;
    Policy.rules().then(
      (all) => { if (live) { setRules((all ?? []).filter((rule) => isBotActor(rule.actor))); setError(null); } },
      (failure: unknown) => { if (live) setError(errText(failure)); },
    );
    // Names only make the list readable; without them each rule shows its bot id.
    Chat.bots().then(
      (bots) => { if (live) setNames(Object.fromEntries((bots ?? []).map((bot) => [`bot:${bot.id}`, bot.name]))); },
      () => {},
    );
    return () => { live = false; };
  }, [revision]);
  if (!inDesktop) return null;
  return <div className="flex flex-col gap-1 px-3 py-2.5">
    <span className="block">Remembered approvals</span>
    <p className="text-xs text-muted-foreground">What you allowed or blocked for a bot always. Removing one makes that bot ask again.</p>
    {error && <p role="alert" className="text-xs text-destructive-foreground">{error} <Button type="button" variant="link" size="xs" onClick={() => setRevision((value) => value + 1)}>Retry</Button></p>}
    {rules?.length === 0 && <p className="text-xs text-muted-foreground">None yet.</p>}
    {rules && rules.length > 0 && <ul className="mt-1 flex flex-col divide-y divide-border/60 rounded-lg border">
      {rules.map((rule) => <li key={rule.id} className="flex items-center justify-between gap-3 px-2.5 py-1.5 text-xs">
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="truncate"><span className="font-medium">{names[rule.actor] ?? rule.actor.slice(4)}</span> · {botRequestTitle(rule.action, rule.resource)}</span>
          <span className="selectable truncate font-mono text-muted-foreground">{DECISIONS[rule.decision] ?? rule.decision} · {rule.action} · {rule.resource}</span>
        </span>
        <Button type="button" variant="outline" size="xs" disabled={removal.busy} onClick={() => void removal.run(async () => { await Policy.deleteRule(rule.id); setRules((list) => (list ?? []).filter((item) => item.id !== rule.id)); })}>Remove</Button>
      </li>)}
    </ul>}
    {removal.error && <p role="alert" className="text-xs text-destructive-foreground">{removal.error}</p>}
  </div>;
}
