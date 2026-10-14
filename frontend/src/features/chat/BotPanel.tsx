import { BookOpenIcon, CpuIcon, GaugeIcon, PencilIcon, PlugIcon, ScrollTextIcon, ShieldIcon, UsersIcon, WrenchIcon, XIcon } from "lucide-react";
import { useEffect, useMemo, useReducer, useRef, useState } from "react";
import { BotAvatar } from "@/components/shell/BotAvatar";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectGroup, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { errText } from "@/features/projects/dialogs";
import { avatarSeed } from "@/lib/botAvatar";
import { Chat, type Bot } from "@/mygo";
import { useChat } from "@/stores/chat";
import { useUI } from "@/stores/ui";
import { fingerprint, formFromBot, formReducer, toInput, validate, type FormState } from "./builder/botForm";
import { Block } from "./builder/controls";
import { ModelPicker } from "./builder/ModelPicker";
import { useModelOptions, type Option } from "./builder/modelOptions";
import { IntegrationsPage, SkillsPage, ToolsPage } from "./builder/ToolsTab";
import { MemoriesPage, ownScope, useBotMemory } from "./panel/MemoriesPage";
import { PermissionsPage } from "./panel/PermissionsPage";
import { ControlRow, Group, NavRow, PageHeader, SwitchRow } from "./panel/rows";

type Page = "main" | "memories" | "skills" | "tools" | "integrations" | "permissions";

/** How long a change waits for further typing before it is saved. */
const SAVE_DELAY_MS = 600;

/** The Brain group: which model the bot thinks with and how hard. */
function Brain({ form, patch }: { form: FormState; patch: (p: Partial<FormState>) => void }) {
  const models = useModelOptions(form.model);
  const { connected, current, loading, failed } = models;
  const levels = current.reasoning;
  const choose = (option: Option | null) => {
    if (option) patch({ model: option.value, reasoning: option.reasoning.includes(form.reasoning) ? form.reasoning : "" });
  };
  return <div className="flex flex-col gap-1.5">
    <Group title="Brain">
      {connected.length === 0
        ? <ControlRow icon={CpuIcon} label="Model">
          <Button type="button" size="xs" variant="outline" onClick={() => useUI.getState().openSettings("ai")}>Connect a provider</Button>
        </ControlRow>
        : <ControlRow icon={CpuIcon} label="Model" htmlFor="bot-model"><ModelPicker id="bot-model" compact models={models} onPick={choose} /></ControlRow>}
      {levels.length > 0 && <ControlRow icon={GaugeIcon} label="Reasoning" htmlFor="bot-reasoning">
        <Select value={form.reasoning} items={[{ value: "", label: "Default" }, ...levels.map((l) => ({ value: l, label: l }))]} onValueChange={(v) => { if (v !== null) patch({ reasoning: v }); }}>
          <SelectTrigger id="bot-reasoning" size="sm" className="w-auto min-w-24 border-0 bg-transparent shadow-none capitalize"><SelectValue /></SelectTrigger>
          <SelectPopup>
            <SelectGroup>
              <SelectItem value="">Default</SelectItem>
              {levels.map((l) => <SelectItem key={l} value={l} className="capitalize">{l}</SelectItem>)}
            </SelectGroup>
          </SelectPopup>
        </Select>
      </ControlRow>}
    </Group>
    {current.kind === "missing" && <p role="status" className="px-1 text-warning-foreground text-xs">This model is no longer offered. Pick another.</p>}
    {loading && <p role="status" className="px-1 text-muted-foreground text-xs">Loading models…</p>}
    {failed.length > 0 && <p role="alert" className="px-1 text-destructive-foreground text-xs">{failed.map((p) => p.name).join(", ")} could not list models.</p>}
  </div>;
}

/**
 * One bot's settings beside its conversation, laid out like a contact card: who it is (its name),
 * how it thinks (Brain), what it knows (memories, skills) and what it can do (tools, integrations, working with
 * other bots). Rows that need room open a page of their own. Every valid change is saved automatically a moment after
 * the last edit.
 */
function PanelBody({ bot }: { bot: Bot }) {
  const [form, dispatch] = useReducer(formReducer, bot, formFromBot);
  const [baseline, setBaseline] = useState<FormState>(form);
  const [page, setPage] = useState<Page>("main");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // The content that last failed to save: it is not retried until the user changes something.
  const [failed, setFailed] = useState("");
  const memory = useBotMemory(bot.id);

  const problems = useMemo(() => validate(form), [form]);
  const invalid = Object.keys(problems).length > 0;
  const dirty = useMemo(() => fingerprint(form) !== fingerprint(baseline), [form, baseline]);
  const patch = (p: Partial<FormState>) => dispatch({ type: "patch", patch: p });

  // The bot can change under the panel (it rewrites its own instructions). Take that in unless the user is mid-edit.
  const synced = useRef(bot.updatedAt);
  useEffect(() => {
    if (bot.updatedAt === synced.current) return;
    synced.current = bot.updatedAt;
    if (dirty || saving) return;
    const next = formFromBot(bot);
    dispatch({ type: "load", state: next });
    setBaseline(next);
  }, [bot, dirty, saving]);

  useEffect(() => {
    if (!dirty || invalid || saving || fingerprint(form) === failed) return;
    const timer = setTimeout(() => {
      const snapshot = form;
      setSaving(true);
      setError(null);
      const input = toInput(snapshot, bot.id);
      Chat.saveBot({ ...input, config: { ...input.config, setup: bot.config.setup } })
        .then(() => { setBaseline(snapshot); setFailed(""); })
        .catch((cause: unknown) => { setError(errText(cause)); setFailed(fingerprint(snapshot)); })
        .finally(() => setSaving(false));
    }, SAVE_DELAY_MS);
    return () => clearTimeout(timer);
  }, [form, dirty, invalid, saving, failed, bot.id, bot.config.setup]);

  const back = () => setPage("main");
  const saved = ownScope(memory?.items)?.count ?? 0;

  return <div className="flex min-h-0 flex-1 flex-col">
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4 pb-4">
      {error && <Alert variant="error"><AlertDescription className="selectable break-words">{error}</AlertDescription></Alert>}
      {page === "main" && <>
        <div className="relative mx-auto">
          <BotAvatar name={form.name} avatar={form.avatar} className="size-20" />
          <Button type="button" variant="outline" size="icon-xs" aria-label="Change avatar" className="absolute right-0 bottom-0 rounded-full" onClick={() => patch({ avatar: avatarSeed(`${form.name}${crypto.randomUUID()}`) })}><PencilIcon aria-hidden /></Button>
        </div>
        <Block label="Name" error={problems.name}>
          {(id, describedBy) => <Input id={id} aria-describedby={describedBy} aria-invalid={!!problems.name || undefined} value={form.name} maxLength={160} autoComplete="off" onChange={(e) => patch({ name: e.target.value })} />}
        </Block>
        <Brain form={form} patch={patch} />
        <Group title="Knows">
          <NavRow icon={BookOpenIcon} label="Memories" value={memory?.loaded ? `${saved} saved` : undefined} onOpen={() => setPage("memories")} />
          <NavRow icon={ScrollTextIcon} label="Skills" invalid={!!problems.skills} value={problems.skills ? "Too many" : `${form.skills.length} assigned`} onOpen={() => setPage("skills")} />
        </Group>
        <Group title="Does">
          <NavRow icon={WrenchIcon} label="Tools" value={`${form.tools.length} on`} onOpen={() => setPage("tools")} />
          <NavRow icon={PlugIcon} label="Integrations" invalid={!!problems.mcpServers} value={problems.mcpServers ? "Too many" : `${form.mcpServers.length} on`} onOpen={() => setPage("integrations")} />
          <NavRow icon={ShieldIcon} label="Permissions" value={form.toolAccess || form.taskHarness || form.taskModel || form.taskReasoning || form.taskPermissionMode ? "Own settings" : "Global settings"} onOpen={() => setPage("permissions")} />
        </Group>
        <Group title="Works with">
          <SwitchRow icon={UsersIcon} label="Leads the team" hint="Hands work to your other bots and talks with them to get it done." checked={form.executive} onChange={(executive) => patch({ executive })} />
        </Group>
      </>}
      {page === "memories" && <MemoriesPage botId={bot.id} onBack={back} />}
      {page === "skills" && <>
        <PageHeader title="Skills" onBack={back} />
        <SkillsPage form={form} dispatch={dispatch} problems={problems} />
      </>}
      {page === "tools" && <>
        <PageHeader title="Tools" onBack={back} />
        <ToolsPage form={form} dispatch={dispatch} />
      </>}
      {page === "integrations" && <>
        <PageHeader title="Integrations" onBack={back} />
        <IntegrationsPage form={form} dispatch={dispatch} problems={problems} />
      </>}
      {page === "permissions" && <>
        <PageHeader title="Permissions" onBack={back} />
        <PermissionsPage form={form} patch={patch} />
      </>}
    </div>
  </div>;
}

export function BotPanel({ bot }: { bot: Bot }) {
  return <aside aria-label={`Settings for ${bot.name}`} data-slot="bot-panel" className="flex w-[22rem] max-w-[55%] shrink-0 flex-col border-l">
    <div className="flex h-9 shrink-0 items-center justify-end px-2">
      <Button variant="ghost" size="icon-xs" aria-label="Close settings" onClick={() => useChat.getState().setSettingsOpen(false)}><XIcon aria-hidden /></Button>
    </div>
    <PanelBody key={bot.id} bot={bot} />
  </aside>;
}
