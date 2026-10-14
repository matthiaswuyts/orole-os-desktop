import { BotIcon, CpuIcon, GaugeIcon, ShieldCheckIcon, ShieldIcon } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { inDesktop } from "@/lib/backend";
import { Settings } from "@/mygo";
import { useHarnessOptions } from "@/features/agents/useHarnessOptions";
import {
  PERMISSION_LABELS, TASK_AUTO, TOOL_ACCESS, botHarnesses, botYoloBlocked, fitTaskChoice, harnessName, modelName, permissionModeOf, taskChoices,
  toolAccessLabel, toolAccessOf,
} from "@/features/chat/botTrust";
import { harnessLogo, modelLogo, WithLogo } from "@/features/chat/botLogos";
import type { AgentsSetting } from "@/features/settings/AgentSafetySettings";
import type { FormState } from "../builder/botForm";
import { ControlRow, Group } from "./rows";

type Item = { value: string; label: string; detail?: string; logo?: ReactNode; disabled?: boolean };

/** A compact select inside a panel row; items can carry a logo, and a second line for why one is unavailable. */
function RowSelect({ id, value, items, disabled, onChange, capitalize }: { id: string; value: string; items: Item[]; disabled?: boolean; onChange: (value: string) => void; capitalize?: boolean }) {
  return <Select value={value} items={items} disabled={disabled} onValueChange={(next: string | null) => { if (next !== null) onChange(next); }}>
    <SelectTrigger id={id} size="sm" className={`w-auto min-w-24 max-w-52 border-0 bg-transparent shadow-none ${capitalize ? "capitalize" : ""}`}><SelectValue>
      {(current: string) => {
        const item = items.find((entry) => entry.value === current);
        return item ? <WithLogo logo={item.logo}>{item.label}</WithLogo> : current;
      }}
    </SelectValue></SelectTrigger>
    <SelectPopup alignItemWithTrigger={false} className="w-72 max-w-[calc(100vw-2rem)]">
      {items.map((item) => <SelectItem key={item.value} value={item.value} disabled={item.disabled} className="items-start py-1.5">
        <span className="flex min-w-0 flex-col gap-0.5">
          <WithLogo logo={item.logo}><span className={capitalize ? "capitalize" : undefined}>{item.label}</span></WithLogo>
          {item.detail && <span className="text-muted-foreground text-xs">{item.detail}</span>}
        </span>
      </SelectItem>)}
    </SelectPopup>
  </Select>;
}

function Hint({ children }: { children: ReactNode }) {
  return <p className="px-1 text-muted-foreground text-xs">{children}</p>;
}

/** Keeps an option for a stored value the current list no longer offers, so the select still shows it. */
function withStored(items: Item[], value: string): Item[] {
  return items.some((item) => item.value === value) ? items : [...items, { value, label: value }];
}

/**
 * What this bot may do without asking and how its code tasks run, each defaulting to the global choice under Settings › Task permissions, Bot tools and Bot code tasks.
 * The daemon applies these; per-integration "Always ask" and "Block" settings still win over any level.
 */
export function PermissionsPage({ form, patch }: { form: FormState; patch: (p: Partial<FormState>) => void }) {
  const [agents, setAgents] = useState<AgentsSetting | null>(null);
  useEffect(() => {
    if (!inDesktop) return;
    let live = true;
    Settings.all().then((all) => { if (live) setAgents((all.agents as AgentsSetting | undefined) ?? {}); }, () => {});
    return () => { live = false; };
  }, []);
  const source = useHarnessOptions("local");
  const harnesses = botHarnesses(source.options?.harnesses);

  const globalAccess = toolAccessOf(agents?.botToolAccess) || "ask";
  const accessItems: Item[] = [
    { value: "", label: agents ? `Use global setting (${toolAccessLabel(globalAccess)})` : "Use global setting" },
    ...TOOL_ACCESS.map((level) => ({ value: level.value, label: level.label })),
  ];
  const accessHint = TOOL_ACCESS.find((level) => level.value === (toolAccessOf(form.toolAccess) || globalAccess))!.hint;

  const globalTasks = agents?.botTasks ?? {};
  const global = { harness: globalTasks.harness || TASK_AUTO, model: globalTasks.model || TASK_AUTO, reasoning: globalTasks.reasoning || TASK_AUTO };
  const harness = form.taskHarness || global.harness;
  const model = form.taskModel || global.model;
  const reasoning = form.taskReasoning || global.reasoning;
  const { models, levels } = taskChoices(harnesses, harness, model);
  const inherit = (name: string) => agents ? `Use global setting (${name})` : "Use global setting";

  const harnessItems = withStored([
    { value: "", label: inherit(harnessName(harnesses, global.harness)), logo: harnessLogo(global.harness) },
    { value: TASK_AUTO, label: "Auto — the bot chooses" },
    ...harnesses.filter((h) => h.available).map((h) => ({ value: h.id, label: h.name || h.id, logo: harnessLogo(h.id) })),
  ], form.taskHarness);
  const globalModel = taskChoices(harnesses, global.harness, global.model).models.find((m) => m.id === global.model);
  const modelItems = withStored([
    { value: "", label: inherit(modelName(harnesses, global.harness, global.model)), logo: globalModel && modelLogo(global.harness, globalModel) },
    { value: TASK_AUTO, label: "Auto — the bot chooses" },
    ...models.map((m) => ({ value: m.id, label: m.name || m.id, logo: modelLogo(harness, m) })),
  ], form.taskModel);
  const levelItems = withStored([
    { value: "", label: inherit(global.reasoning === TASK_AUTO ? "Auto" : global.reasoning) },
    { value: TASK_AUTO, label: "Auto — the bot chooses" },
    ...levels.map((level) => ({ value: level, label: level })),
  ], form.taskReasoning);

  const yoloOff = !!agents?.yoloDisabled;
  const defaultMode = agents?.defaultPermissionMode === "auto" || (agents?.defaultPermissionMode === "yolo" && !yoloOff) ? agents.defaultPermissionMode : "manual";
  const globalMode = permissionModeOf(globalTasks.permissionMode) || defaultMode;
  // Before the global setting arrives nothing is known to block Yolo; the daemon refuses it anyway when it is off.
  const yoloBlocked = agents ? botYoloBlocked(agents) : "";
  const modeItems: Item[] = [
    { value: "", label: inherit(PERMISSION_LABELS[globalMode]) },
    { value: "manual", label: "Manual" },
    { value: "auto", label: "Auto" },
    { value: "yolo", label: "Yolo", detail: yoloBlocked || undefined, disabled: !!yoloBlocked },
  ];
  const mode = permissionModeOf(form.taskPermissionMode) || globalMode;

  /** A harness the current model or effort does not fit resets those to Auto, so the bot is never told to use a model its agent lacks. */
  const chooseHarness = (next: string) => {
    const fit = fitTaskChoice(harnesses, { harness: next || global.harness, model, reasoning });
    patch({ taskHarness: next, taskModel: fit.model === model ? form.taskModel : fit.model, taskReasoning: fit.reasoning === reasoning ? form.taskReasoning : fit.reasoning });
  };
  const chooseModel = (next: string) => {
    const fit = fitTaskChoice(harnesses, { harness, model: next || global.model, reasoning });
    patch({ taskModel: next, taskReasoning: fit.reasoning === reasoning ? form.taskReasoning : fit.reasoning });
  };

  return <div className="flex flex-col gap-4">
    <div className="flex flex-col gap-1.5">
      <Group title="Integrations and tools">
        <ControlRow icon={ShieldIcon} label="Without asking" htmlFor="bot-tool-access">
          <RowSelect id="bot-tool-access" value={toolAccessOf(form.toolAccess)} items={accessItems} onChange={(toolAccess) => patch({ toolAccess })} />
        </ControlRow>
      </Group>
      <Hint>{accessHint} An integration tool you set to Always ask or Block still asks or stays blocked.</Hint>
    </div>
    {form.codeTasks
      ? <div className="flex flex-col gap-1.5">
        <Group title="Code tasks">
          <ControlRow icon={BotIcon} label="Agent" htmlFor="bot-task-harness">
            <RowSelect id="bot-task-harness" value={form.taskHarness} items={harnessItems} onChange={chooseHarness} />
          </ControlRow>
          <ControlRow icon={CpuIcon} label="Model" htmlFor="bot-task-model">
            <RowSelect id="bot-task-model" value={form.taskModel} items={modelItems} disabled={harness === TASK_AUTO && form.taskModel === ""} onChange={chooseModel} />
          </ControlRow>
          <ControlRow icon={GaugeIcon} label="Effort" htmlFor="bot-task-reasoning">
            <RowSelect id="bot-task-reasoning" value={form.taskReasoning} items={levelItems} capitalize disabled={harness === TASK_AUTO && form.taskReasoning === ""} onChange={(taskReasoning) => patch({ taskReasoning })} />
          </ControlRow>
          <ControlRow icon={ShieldCheckIcon} label="Permission mode" htmlFor="bot-task-mode">
            <RowSelect id="bot-task-mode" value={permissionModeOf(form.taskPermissionMode)} items={modeItems} onChange={(taskPermissionMode) => patch({ taskPermissionMode })} />
          </ControlRow>
        </Group>
        <Hint>
          {harness === TASK_AUTO ? "Pick an agent to fix its model and effort. " : ""}
          Auto lets the bot choose for each task. The bot never picks the permission mode.
          {mode === "yolo" && yoloBlocked ? ` Its starts in Yolo are refused right now: ${yoloBlocked}` : ""}
        </Hint>
      </div>
      : <Hint>Turn on Runs code tasks under Tools to choose how this bot’s code tasks run.</Hint>}
  </div>;
}
