import { useEffect, useState, type ReactNode } from "react";
import { create } from "zustand";
import { Chat, Policy, Settings, type BotTaskDefaults, type Rule } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { Button } from "@/components/ui/button";
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { errText, useAction } from "@/features/agents/format";
import { useHarnessOptions } from "@/features/agents/useHarnessOptions";
import { harnessLogo, modelLogo, WithLogo } from "@/features/chat/botLogos";
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

type AgentsStore = { setting: AgentsSetting | null; error: string | null; load: () => void; change: (patch: AgentsSetting) => Promise<void> };

/**
 * The daemon reads the "agents" setting on every task start, mode change, title request and bot tool call, so changes take
 * effect at once. The value is one JSON object shared by every control on the Task permissions, Bot tools and Bot code tasks
 * pages (and the task naming switch under System): each save writes the whole object back so it never drops a key another
 * control (or a newer daemon) owns.
 */
export const useAgentsSetting = create<AgentsStore>((set, get) => ({
  setting: null,
  error: null,
  load: () => {
    if (!inDesktop) return;
    Settings.all().then(
      (all) => set({ setting: (all.agents as AgentsSetting | undefined) ?? {} }),
      (failure: unknown) => set({ error: errText(failure) }),
    );
  },
  change: async (patch) => {
    const current = get().setting;
    if (!current) return;
    set({ error: null });
    const next = { ...current, ...patch };
    try { await Settings.set("agents", next); set({ setting: next }); }
    catch (failure) { set({ error: errText(failure) }); }
  },
}));

/** The shared setting, loaded when a page using it opens. */
function useAgents() {
  const { setting, error, load, change } = useAgentsSetting();
  useEffect(load, [load]);
  const yoloOff = !!setting?.yoloDisabled;
  const defaultMode: PermissionMode = setting?.defaultPermissionMode === "auto" || (setting?.defaultPermissionMode === "yolo" && !yoloOff) ? setting.defaultPermissionMode : "manual";
  return { setting, change: (patch: AgentsSetting) => void change(patch), disabled: !inDesktop || setting === null, yoloOff, defaultMode, error };
}

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

function SwitchRow({ id, label, hint, checked, disabled, onChange }: { id: string; label: string; hint?: ReactNode; checked: boolean; disabled: boolean; onChange: (on: boolean) => void }) {
  return <div className="flex items-center justify-between gap-6 px-3 py-2.5">
    <div className="min-w-0">
      <label htmlFor={id} className="block">{label}</label>
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </div>
    <Switch id={id} checked={checked} disabled={disabled} onCheckedChange={onChange} aria-label={label} />
  </div>;
}

function ErrorLine({ error }: { error: string | null }) {
  return error ? <p role="alert" className="-mt-3 mb-5 px-3 text-xs text-destructive-foreground">{error}</p> : null;
}

/** Settings › Task permissions: how tasks start and what bots may do with them. */
export function TaskPermissionsSettings() {
  const { setting, change, disabled, yoloOff, defaultMode, error } = useAgents();
  const tasks = setting?.botTasks ?? {};
  const botMode = permissionModeOf(tasks.permissionMode);
  const yoloBlocked = botYoloBlocked(setting);
  const botModeItems = [{ value: "", label: `Same as my default (${PERMISSION_LABELS[defaultMode]})` }, ...MODES];
  return <>
    <SettingsSection id="settings-task-modes" title="Task modes" description="Manual, Auto and Yolo are chosen per task; the default applies when nobody picked one.">
      <SwitchRow id="allow-yolo" label="Allow Yolo mode" hint="Yolo runs a task without ever asking. Off blocks starting or switching to it; tasks already in Yolo must be switched." checked={!yoloOff} disabled={disabled} onChange={(on) => change({ yoloDisabled: !on })} />
      <SelectRow id="default-permission-mode" label="Default permission mode" hint={yoloOff ? "Yolo is turned off above." : undefined}>
        <Select value={defaultMode} items={MODES} disabled={disabled} onValueChange={(mode: PermissionMode | null) => { if (mode) change({ defaultPermissionMode: mode }); }}>
          <SelectTrigger aria-labelledby="default-permission-mode-label" className="w-40 shrink-0"><SelectValue /></SelectTrigger>
          <SelectPopup>
            {MODES.map((mode) => <SelectItem key={mode.value} value={mode.value} disabled={mode.value === "yolo" && yoloOff}>{mode.label}</SelectItem>)}
          </SelectPopup>
        </Select>
      </SelectRow>
    </SettingsSection>
    <SettingsSection id="settings-bot-permissions" title="Bots" description="A bot's own settings can choose differently.">
      <SelectRow id="bot-task-mode" label="Bot permission mode" hint={botMode === "yolo" && yoloBlocked ? `Bots cannot start tasks in Yolo right now, so their starts are refused. ${yoloBlocked}` : "Bots never choose this themselves."}>
        <Select value={botMode} items={botModeItems} disabled={disabled} onValueChange={(next: PermissionMode | "" | null) => { if (next !== null) change({ botTasks: { ...tasks, permissionMode: next } }); }}>
          <SelectTrigger aria-labelledby="bot-task-mode-label" className="w-56 shrink-0"><SelectValue /></SelectTrigger>
          <SelectPopup alignItemWithTrigger={false} className="w-72">
            {botModeItems.map((item) => {
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
      <SwitchRow id="bot-yolo" label="Bots may use Yolo" hint={<>Lets a bot start a task in Yolo.{yoloOff ? " Needs Yolo mode allowed above." : ""}</>} checked={!!setting?.botYolo && !yoloOff} disabled={disabled || yoloOff} onChange={(on) => change({ botYolo: on })} />
      <SwitchRow id="bot-auto-approve" label="Bots start tasks without asking" hint="Off: every start and follow-up waits for your approval in the bot's chat, unless you allowed that bot always." checked={!!setting?.botAutoApprove} disabled={disabled} onChange={(on) => change({ botAutoApprove: on })} />
    </SettingsSection>
    <SettingsSection id="settings-remembered-approvals" title="Remembered approvals" description="What you allowed or blocked for a bot always. Removing one makes that bot ask again."><RememberedApprovals /></SettingsSection>
    <ErrorLine error={error} />
  </>;
}

/** Settings › Bot tools: how much bots may do through integrations and tools before they ask. */
export function BotToolsSettings() {
  const { setting, change, disabled, error } = useAgents();
  const access = toolAccessOf(setting?.botToolAccess) || "ask";
  return <>
    <SettingsSection id="settings-bot-tools" title="Approvals" description="A bot's own settings can choose differently. A tool set to Always ask or Block still asks or stays blocked.">
      <SelectRow id="bot-tool-access" label="Without asking, bots may" hint={TOOL_ACCESS.find((level) => level.value === access)!.hint}>
        <Select value={access} items={TOOL_ACCESS} disabled={disabled} onValueChange={(level: ToolAccess | null) => { if (level) change({ botToolAccess: level }); }}>
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
    <ErrorLine error={error} />
  </>;
}

type Choice = { value: string; label: string; logo?: ReactNode };

/** Settings › Bot code tasks: which agent, model and effort the code tasks bots start run with. */
export function BotCodeTasksSettings() {
  const { setting, change, disabled, error } = useAgents();
  const source = useHarnessOptions("local");
  const harnesses = botHarnesses(source.options?.harnesses);
  const tasks = setting?.botTasks ?? {};
  const harness = tasks.harness || TASK_AUTO;
  const model = tasks.model || TASK_AUTO;
  const reasoning = tasks.reasoning || TASK_AUTO;
  const { models, levels } = taskChoices(harnesses, harness, model);
  const save = (patch: BotTaskDefaults) => change({ botTasks: { ...tasks, ...patch } });

  const installed = harnesses.filter((h) => h.available);
  const harnessItems: Choice[] = [{ value: TASK_AUTO, label: "Auto — the bot chooses" }, ...installed.map((h) => ({ value: h.id, label: h.name || h.id, logo: harnessLogo(h.id) }))];
  if (!harnessItems.some((item) => item.value === harness)) harnessItems.push({ value: harness, label: `${harnesses.find((h) => h.id === harness)?.name || harness} (not installed)`, logo: harnessLogo(harness) });
  const modelItems: Choice[] = [{ value: TASK_AUTO, label: harness === TASK_AUTO ? "Auto" : "Auto — the bot chooses" }, ...models.map((m) => ({ value: m.id, label: m.name || m.id, logo: modelLogo(harness, m) }))];
  if (!modelItems.some((item) => item.value === model)) modelItems.push({ value: model, label: model });
  const levelItems = [{ value: TASK_AUTO, label: harness === TASK_AUTO ? "Auto" : "Auto — the bot chooses" }, ...levels.map((level) => ({ value: level, label: level }))];
  if (!levelItems.some((item) => item.value === reasoning)) levelItems.push({ value: reasoning, label: reasoning });
  const discovering = source.loading ? "Looking for installed agents…" : source.error ? `Could not list installed agents: ${source.error}` : undefined;

  /** The closed select value, with the logo of the chosen option. */
  const withLogo = (items: Choice[]) => (value: string) => {
    const item = items.find((entry) => entry.value === value);
    return <WithLogo logo={item?.logo}>{item?.label ?? value}</WithLogo>;
  };
  const logoItems = (items: Choice[]) => items.map((item) => <SelectItem key={item.value} value={item.value}><WithLogo logo={item.logo}>{item.label}</WithLogo></SelectItem>);

  return <>
    <SettingsSection id="settings-bot-tasks" title="Run with" description="Auto lets the bot choose for each task. A bot's own settings can choose differently.">
      <SelectRow id="bot-task-harness" label="Agent" hint={discovering}>
        <Select value={harness} items={harnessItems} disabled={disabled} onValueChange={(next: string | null) => { if (next) save({ harness: next, ...fitTaskChoice(harnesses, { harness: next, model, reasoning }) }); }}>
          <SelectTrigger aria-labelledby="bot-task-harness-label" className="w-56 shrink-0"><SelectValue>{withLogo(harnessItems)}</SelectValue></SelectTrigger>
          <SelectPopup>{logoItems(harnessItems)}</SelectPopup>
        </Select>
      </SelectRow>
      <SelectRow id="bot-task-model" label="Model" hint={harness === TASK_AUTO ? "Pick an agent first to fix its model." : undefined}>
        <Select value={model} items={modelItems} disabled={disabled || harness === TASK_AUTO} onValueChange={(next: string | null) => { if (next) save({ model: next, reasoning: fitTaskChoice(harnesses, { harness, model: next, reasoning }).reasoning }); }}>
          <SelectTrigger aria-labelledby="bot-task-model-label" className="w-56 shrink-0"><SelectValue>{withLogo(modelItems)}</SelectValue></SelectTrigger>
          <SelectPopup>{logoItems(modelItems)}</SelectPopup>
        </Select>
      </SelectRow>
      <SelectRow id="bot-task-reasoning" label="Effort" hint={levels.length === 0 && harness !== TASK_AUTO ? "This agent reports no effort levels." : undefined}>
        <Select value={reasoning} items={levelItems} disabled={disabled || (levels.length === 0 && reasoning === TASK_AUTO)} onValueChange={(next: string | null) => { if (next) save({ reasoning: next }); }}>
          <SelectTrigger aria-labelledby="bot-task-reasoning-label" className="w-56 shrink-0 capitalize"><SelectValue /></SelectTrigger>
          <SelectPopup>{levelItems.map((item) => <SelectItem key={item.value} value={item.value} className="capitalize">{item.label}</SelectItem>)}</SelectPopup>
        </Select>
      </SelectRow>
    </SettingsSection>
    <ErrorLine error={error} />
  </>;
}

/** Settings › System › Tasks: whether new tasks get an AI-written title. */
export function TaskNamingSettings() {
  const { setting, change, disabled, error } = useAgents();
  return <>
    <SettingsSection id="settings-task-naming" title="Tasks">
      <SwitchRow id="ai-task-titles" label="Name tasks with AI" hint="Uses your Claude or ChatGPT account for one short extra request per new task. Oh My Pi names its own tasks." checked={!setting?.aiTitlesDisabled} disabled={disabled} onChange={(on) => change({ aiTitlesDisabled: !on })} />
    </SettingsSection>
    <ErrorLine error={error} />
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
    {error && <p role="alert" className="text-xs text-destructive-foreground">{error} <Button type="button" variant="link" size="xs" onClick={() => setRevision((value) => value + 1)}>Retry</Button></p>}
    {rules?.length === 0 && <p className="text-xs text-muted-foreground">None yet.</p>}
    {rules && rules.length > 0 && <ul className="flex flex-col divide-y divide-border/60 rounded-lg border">
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
