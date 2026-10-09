import { XIcon } from "lucide-react";
import { useMemo, useReducer, useState } from "react";
import { BotAvatar } from "@/components/shell/BotAvatar";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsiblePanel, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Tabs, TabsList, TabsPanel, TabsTab } from "@/components/ui/tabs";
import { errText } from "@/features/projects/dialogs";
import { Chat, type Bot } from "@/mygo";
import { useChat } from "@/stores/chat";
import { fingerprint, formFromBot, formReducer, tabOfProblem, toInput, validate, type FormState, type TabId } from "./builder/botForm";
import { KnowledgeTab } from "./builder/KnowledgeTab";
import { MemoryTab } from "./builder/MemoryTab";
import { ModelTab } from "./builder/ModelTab";
import { SkillProposals } from "./builder/SkillProposals";
import { ToolsTab } from "./builder/ToolsTab";

const TABS: { id: TabId; label: string }[] = [
  { id: "persona", label: "Persona" },
  { id: "tools", label: "Tools" },
  { id: "knowledge", label: "Knowledge" },
];
const isTab = (value: unknown): value is TabId => TABS.some((t) => t.id === value);

const bare = "w-full rounded-md bg-transparent px-2 py-1 text-center outline-none hover:bg-accent focus-visible:bg-accent focus-visible:ring-2 focus-visible:ring-ring";

/**
 * One bot's settings beside its conversation, in three tabs: Persona (name, description, model), Tools and
 * Knowledge (memory and documents). The instructions are not edited here: the bot writes and updates them itself.
 */
function PanelBody({ bot }: { bot: Bot }) {
  const [form, dispatch] = useReducer(formReducer, bot, formFromBot);
  const [baseline, setBaseline] = useState<FormState>(form);
  const [tab, setTab] = useState<TabId>("persona");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const problems = useMemo(() => validate(form), [form]);
  const dirty = useMemo(() => fingerprint(form) !== fingerprint(baseline), [form, baseline]);
  const tabsWithProblems = new Set(Object.keys(problems).map(tabOfProblem));
  const patch = (p: Partial<FormState>) => dispatch({ type: "patch", patch: p });

  async function save() {
    if (saving || Object.keys(problems).length > 0) return;
    const snapshot = form;
    setSaving(true);
    setError(null);
    try {
      const input = toInput(snapshot, bot.id);
      await Chat.saveBot({ ...input, config: { ...input.config, setup: bot.config.setup } });
      setBaseline(snapshot);
    } catch (cause) {
      setError(errText(cause));
    } finally {
      setSaving(false);
    }
  }

  return <div className="flex min-h-0 flex-1 flex-col">
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4 pb-4">
      <div className="flex flex-col items-center gap-2">
        <BotAvatar name={form.name} avatar={bot.avatar} className="size-20" />
        <div className="flex w-full flex-col items-center gap-0.5">
          <input aria-label="Name" value={form.name} maxLength={160} autoComplete="off" aria-invalid={!!problems.name || undefined} onChange={(e) => patch({ name: e.target.value })} className={`${bare} font-medium text-base`} />
          <input aria-label="Description" value={form.description} placeholder="Add a description" maxLength={600} autoComplete="off" onChange={(e) => patch({ description: e.target.value })} className={`${bare} text-muted-foreground text-xs placeholder:text-muted-foreground/70`} />
        </div>
        {(problems.name || problems.description) && <p role="alert" className="text-destructive-foreground text-xs">{problems.name ?? problems.description}</p>}
      </div>
      {error && <Alert variant="error"><AlertDescription className="selectable break-words">{error}</AlertDescription></Alert>}
      <Tabs value={tab} onValueChange={(value) => { if (isTab(value)) setTab(value); }}>
        <TabsList className="mx-auto" aria-label="Bot settings">
          {TABS.map((t) => (
            <TabsTab key={t.id} value={t.id}>
              {t.label}
              {tabsWithProblems.has(t.id) && <span role="img" aria-label="has problems" className="ms-1.5 size-1.5 rounded-full bg-destructive" />}
            </TabsTab>
          ))}
        </TabsList>
        <TabsPanel value="persona">
          <div className="flex flex-col gap-3">
            <ModelTab form={form} dispatch={dispatch} />
            <Collapsible className="text-xs text-muted-foreground">
              <CollapsibleTrigger className="rounded-md py-0.5 font-medium outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring">Instructions</CollapsibleTrigger>
              <CollapsiblePanel>
                <p className="selectable mt-1 max-h-60 overflow-y-auto whitespace-pre-wrap break-words rounded-lg border bg-card p-2">{form.instructions || "None yet."}</p>
              </CollapsiblePanel>
            </Collapsible>
          </div>
        </TabsPanel>
        <TabsPanel value="tools">
          <div className="flex flex-col gap-3">
            <ToolsTab form={form} dispatch={dispatch} problems={problems} />
            <SkillProposals botId={bot.id} />
          </div>
        </TabsPanel>
        <TabsPanel value="knowledge" keepMounted>
          <div className="flex flex-col gap-3">
            <MemoryTab botId={bot.id} form={form} dispatch={dispatch} />
            <KnowledgeTab botId={bot.id} form={form} dispatch={dispatch} />
          </div>
        </TabsPanel>
      </Tabs>
    </div>
    {dirty && <div className="flex shrink-0 items-center gap-2 border-t px-4 py-2">
      <Button size="sm" variant="outline" disabled={saving} onClick={() => { dispatch({ type: "load", state: baseline }); setError(null); }}>Discard</Button>
      <Button size="sm" className="flex-1" disabled={saving || Object.keys(problems).length > 0} onClick={() => void save()}>{saving ? "Saving…" : "Save changes"}</Button>
    </div>}
  </div>;
}

export function BotPanel({ bot }: { bot: Bot }) {
  return <aside aria-label={`Settings for ${bot.name}`} data-slot="bot-panel" className="flex w-[26rem] max-w-[55%] shrink-0 flex-col border-l">
    <div className="flex h-9 shrink-0 items-center justify-end px-2">
      <Button variant="ghost" size="icon-xs" aria-label="Close settings" onClick={() => useChat.getState().setSettingsOpen(false)}><XIcon aria-hidden /></Button>
    </div>
    <PanelBody key={`${bot.id}:${bot.updatedAt}`} bot={bot} />
  </aside>;
}
