import { XIcon } from "lucide-react";
import { useEffect, useMemo, useReducer, useRef, useState } from "react";
import { BotAvatar } from "@/components/shell/BotAvatar";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsPanel, TabsTab } from "@/components/ui/tabs";
import { errText } from "@/features/projects/dialogs";
import { Chat, type Bot } from "@/mygo";
import { useChat } from "@/stores/chat";
import { fingerprint, formFromBot, formReducer, tabOfProblem, toInput, validate, type FormState, type TabId } from "./builder/botForm";
import { ModelTab } from "./builder/ModelTab";
import { SkillProposals } from "./builder/SkillProposals";
import { ToolsTab } from "./builder/ToolsTab";

const TABS: { id: TabId; label: string }[] = [
  { id: "persona", label: "Persona" },
  { id: "tools", label: "Tools" },
];
const isTab = (value: unknown): value is TabId => TABS.some((t) => t.id === value);

const bare = "w-full rounded-md bg-transparent px-2 py-1 text-center outline-none hover:bg-accent focus-visible:bg-accent focus-visible:ring-2 focus-visible:ring-ring";

/** How long a change waits for further typing before it is saved. */
const SAVE_DELAY_MS = 600;

/**
 * One bot's settings beside its conversation, in two tabs: Persona (name, description, model) and Tools. Every valid
 * change is saved automatically a moment after the last edit. Memory and knowledge need no settings: each bot always
 * reads and writes its own. The instructions are neither shown nor edited here: the bot writes and updates them itself.
 */
function PanelBody({ bot }: { bot: Bot }) {
  const [form, dispatch] = useReducer(formReducer, bot, formFromBot);
  const [baseline, setBaseline] = useState<FormState>(form);
  const [tab, setTab] = useState<TabId>("persona");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // The content that last failed to save: it is not retried until the user changes something.
  const [failed, setFailed] = useState("");

  const problems = useMemo(() => validate(form), [form]);
  const invalid = Object.keys(problems).length > 0;
  const dirty = useMemo(() => fingerprint(form) !== fingerprint(baseline), [form, baseline]);
  const tabsWithProblems = new Set(Object.keys(problems).map(tabOfProblem));
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
          <ModelTab form={form} dispatch={dispatch} />
        </TabsPanel>
        <TabsPanel value="tools">
          <div className="flex flex-col gap-3">
            <ToolsTab form={form} dispatch={dispatch} problems={problems} />
            <SkillProposals botId={bot.id} />
          </div>
        </TabsPanel>
      </Tabs>
    </div>
    <p role="status" className="shrink-0 border-t px-4 py-1.5 text-muted-foreground text-xs">
      {saving ? "Saving…" : invalid ? "Fix the highlighted fields to save." : dirty ? (fingerprint(form) === failed ? "Not saved. Change something to try again." : "Saving soon…") : "All changes saved."}
    </p>
  </div>;
}

export function BotPanel({ bot }: { bot: Bot }) {
  return <aside aria-label={`Settings for ${bot.name}`} data-slot="bot-panel" className="flex w-[26rem] max-w-[55%] shrink-0 flex-col border-l">
    <div className="flex h-9 shrink-0 items-center justify-end px-2">
      <Button variant="ghost" size="icon-xs" aria-label="Close settings" onClick={() => useChat.getState().setSettingsOpen(false)}><XIcon aria-hidden /></Button>
    </div>
    <PanelBody key={bot.id} bot={bot} />
  </aside>;
}
