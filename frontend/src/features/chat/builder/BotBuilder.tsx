import { useMemo, useReducer, useState } from "react";
import { PanelRight, X } from "lucide-react";
import { Chat, type Bot } from "@/mygo";
import { BotAvatar } from "@/components/shell/BotAvatar";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsPanel, TabsTab } from "@/components/ui/tabs";
import { toastManager } from "@/components/ui/toast";
import { ConfirmDialog, errText } from "@/features/projects/dialogs";
import { NeedsDesktop } from "@/features/shared/States";
import { inDesktop } from "@/lib/backend";
import { cn } from "@/lib/utils";
import { useChat } from "@/stores/chat";
import { copyName, fingerprint, formFromBot, formReducer, initialForm, tabOfProblem, toInput, validate, type FormState, type Problems, type TabId } from "./botForm";
import { BuilderMenu } from "./BuilderMenu";
import { KnowledgeTab } from "./KnowledgeTab";
import { MemoryTab } from "./MemoryTab";
import { ModelTab } from "./ModelTab";
import { PersonaTab } from "./PersonaTab";
import { PreviewPane } from "./PreviewPane";
import { SkillProposals } from "./SkillProposals";
import { ToolsTab } from "./ToolsTab";
import { useLoad } from "./useLoad";

const TABS: { id: TabId; label: string }[] = [
  { id: "persona", label: "Persona" },
  { id: "model", label: "Model" },
  { id: "tools", label: "Tools" },
  { id: "memory", label: "Memory" },
  { id: "knowledge", label: "Knowledge" },
];

const isTab = (value: unknown): value is TabId => TABS.some((t) => t.id === value);

type Confirming = "close" | "discard" | "archive" | null;

/** Problems worth showing right now: a blank name is not an error until the user tried to save. */
function visible(problems: Problems, form: FormState, attempted: boolean): Problems {
  if (attempted) return problems;
  return Object.fromEntries(Object.entries(problems).filter(([key]) => !(key === "name" && !form.name.trim())));
}

function Editor({ bot }: { bot: Bot | null }) {
  const [form, dispatch] = useReducer(formReducer, bot, (b) => (b ? formFromBot(b) : initialForm()));
  const [baseline, setBaseline] = useState<FormState>(form);
  const [tab, setTab] = useState<TabId>("persona");
  const [attempted, setAttempted] = useState(false);
  const [saving, setSaving] = useState(false);
  const [daemonError, setDaemonError] = useState<string | null>(null);
  const [confirming, setConfirming] = useState<Confirming>(null);
  const [previewOpen, setPreviewOpen] = useState(true);

  const botId = bot?.id ?? null;
  const problems = useMemo(() => validate(form), [form]);
  const shown = visible(problems, form, attempted);
  const dirty = useMemo(() => fingerprint(form) !== fingerprint(baseline), [form, baseline]);
  const tabsWithProblems = new Set(Object.keys(shown).map(tabOfProblem));
  const title = form.name.trim() || (bot ? bot.name : "New bot");

  /** Shows the problems and moves to the first one; true when the form can be sent. */
  function checked(): boolean {
    setAttempted(true);
    setDaemonError(null);
    const first = Object.keys(problems)[0];
    if (first) setTab(tabOfProblem(first));
    return first === undefined;
  }

  async function save() {
    if (saving || !checked()) return;
    const snapshot = form;
    setSaving(true);
    try {
      const saved = await Chat.saveBot(toInput(snapshot, botId ?? ""));
      if (!bot) {
        useChat.getState().open(saved.id);
        useChat.getState().closeBuilder();
        return;
      }
      setBaseline(snapshot);
      setAttempted(false);
    } catch (cause) {
      setDaemonError(errText(cause));
    } finally {
      setSaving(false);
    }
  }

  async function duplicate() {
    if (saving || !checked()) return;
    setSaving(true);
    try {
      const copy = await Chat.saveBot({ ...toInput(form, ""), name: copyName(form.name) });
      toastManager.add({ type: "success", title: `Created “${copy.name}”`, timeout: 4000 });
      useChat.getState().open(copy.id);
      useChat.getState().closeBuilder();
    } catch (cause) {
      setDaemonError(errText(cause));
      setSaving(false);
    }
  }

  const status = dirty ? "Unsaved changes" : bot ? "All changes saved" : "Not saved yet";

  return (
    <div
      className="flex h-full min-h-0 flex-col"
      onKeyDown={(e) => { if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "s") { e.preventDefault(); void save(); } }}
    >
      <header className="flex flex-wrap items-center gap-2 border-b px-3 py-2">
        <Button size="icon-sm" variant="ghost" aria-label="Close the bot builder" onClick={() => (dirty ? setConfirming("close") : useChat.getState().closeBuilder())}><X /></Button>
        <BotAvatar name={title} avatar={form.avatar} color={form.color} className="size-6 text-xs" />
        <h1 className="min-w-0 flex-1 truncate font-medium text-sm">{title}</h1>
        <span role="status" className={cn("text-xs", dirty ? "text-warning-foreground" : "text-muted-foreground")}>{status}</span>
        <Button size="sm" variant="outline" disabled={!dirty || saving} onClick={() => setConfirming("discard")}>Discard</Button>
        <Button size="sm" disabled={saving || (!dirty && bot !== null)} onClick={() => void save()}>{saving ? "Saving…" : "Save"}</Button>
        <Button size="icon-sm" variant="ghost" aria-label={previewOpen ? "Hide the preview" : "Show the preview"} aria-pressed={previewOpen} onClick={() => setPreviewOpen((o) => !o)}><PanelRight /></Button>
        <BuilderMenu botId={botId} dirty={dirty} disabled={saving} onDuplicate={() => void duplicate()} onArchive={() => setConfirming("archive")} />
      </header>

      <div className="@container flex min-h-0 flex-1 flex-col overflow-y-auto @3xl:overflow-hidden">
        <div className="flex min-h-0 flex-1 flex-col gap-3 p-3 @3xl:flex-row">
          <div className="flex min-w-0 flex-1 flex-col gap-3 @3xl:overflow-y-auto @3xl:pe-1">
            {daemonError && (
              <Alert variant="error">
                <AlertDescription className="flex items-start justify-between gap-2">
                  <span className="selectable break-words">{daemonError}</span>
                  <Button size="xs" variant="ghost" onClick={() => setDaemonError(null)}>Dismiss</Button>
                </AlertDescription>
              </Alert>
            )}
            <Tabs value={tab} onValueChange={(value) => { if (isTab(value)) setTab(value); }}>
              <TabsList variant="underline" className="max-w-full overflow-x-auto" aria-label="Bot settings">
                {TABS.map((t) => (
                  <TabsTab key={t.id} value={t.id}>
                    {t.label}
                    {tabsWithProblems.has(t.id) && <span role="img" aria-label="has problems" className="ms-1.5 size-1.5 rounded-full bg-destructive" />}
                  </TabsTab>
                ))}
              </TabsList>
              <TabsPanel value="persona"><PersonaTab form={form} dispatch={dispatch} problems={shown} /></TabsPanel>
              <TabsPanel value="model"><ModelTab form={form} dispatch={dispatch} problems={shown} /></TabsPanel>
              <TabsPanel value="tools">
                <div className="flex flex-col gap-3">
                  <ToolsTab form={form} dispatch={dispatch} problems={shown} botId={botId} />
                  {botId && <SkillProposals botId={botId} />}
                </div>
              </TabsPanel>
              <TabsPanel value="memory"><MemoryTab botId={botId} form={form} dispatch={dispatch} /></TabsPanel>
              <TabsPanel value="knowledge" keepMounted><KnowledgeTab botId={botId} form={form} dispatch={dispatch} onTab={setTab} /></TabsPanel>
            </Tabs>
          </div>
          {previewOpen && <PreviewPane form={form} problems={problems} className="h-[28rem] shrink-0 @3xl:h-auto @3xl:w-[22rem]" />}
        </div>
      </div>

      <ConfirmDialog
        open={confirming === "close"} onOpenChange={(o) => { if (!o) setConfirming(null); }}
        title="Discard unsaved changes?" confirmLabel="Discard and close"
        onConfirm={async () => { useChat.getState().closeBuilder(); }}
      >
        <p>{bot ? `Your edits to “${bot.name}” have not been saved.` : "This bot has not been created yet."} Closing the builder throws them away.</p>
      </ConfirmDialog>
      <ConfirmDialog
        open={confirming === "discard"} onOpenChange={(o) => { if (!o) setConfirming(null); }}
        title="Discard unsaved changes?" confirmLabel="Discard changes"
        onConfirm={async () => { dispatch({ type: "load", state: baseline }); setAttempted(false); setDaemonError(null); }}
      >
        <p>{bot ? "The form goes back to how this bot was last saved." : "The form is emptied."}</p>
      </ConfirmDialog>
      <ConfirmDialog
        open={confirming === "archive"} onOpenChange={(o) => { if (!o) setConfirming(null); }}
        title={`Archive “${bot?.name ?? ""}”?`} confirmLabel="Archive bot"
        onConfirm={async () => {
          if (!bot) return;
          await Chat.archiveBot(bot.id);
          useChat.getState().closeBuilder();
        }}
      >
        <p>The bot is hidden from your contacts and any reply in progress is stopped. Its conversation and memory are kept on this computer.</p>
      </ConfirmDialog>
    </div>
  );
}

function Shell({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex items-center gap-2 border-b px-3 py-2">
        <Button size="icon-sm" variant="ghost" aria-label="Close the bot builder" onClick={() => useChat.getState().closeBuilder()}><X /></Button>
        <h1 className="font-medium text-sm">Edit bot</h1>
      </header>
      <div className="flex flex-1 flex-col gap-3 p-3">{children}</div>
    </div>
  );
}

/** Creates (`botId` null) or edits a bot. Everything here is a draft until Save; the daemon validates again. */
export function BotBuilder({ botId }: { botId: string | null }) {
  const loaded = useLoad(() => Chat.bot(botId ?? ""), botId ?? "", inDesktop && botId !== null);
  if (!inDesktop) return <NeedsDesktop what="The bot builder" />;
  if (botId === null) return <Editor key="new" bot={null} />;
  if (loaded.data) return <Editor key={loaded.data.id} bot={loaded.data} />;
  return (
    <Shell>
      {loaded.error
        ? (
          <Alert variant="error">
            <AlertDescription className="flex items-center justify-between gap-2">
              <span className="selectable break-words">Could not open this bot: {loaded.error}</span>
              <Button size="xs" variant="outline" onClick={loaded.reload}>Try again</Button>
            </AlertDescription>
          </Alert>
        )
        : <p role="status" className="text-muted-foreground text-sm">Loading the bot…</p>}
    </Shell>
  );
}

export default BotBuilder;
