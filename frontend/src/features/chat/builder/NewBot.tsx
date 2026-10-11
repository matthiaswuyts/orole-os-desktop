import { useState } from "react";
import { X } from "lucide-react";
import { Chat } from "@/mygo";
import { BotAvatar } from "@/components/shell/BotAvatar";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { errText } from "@/features/projects/dialogs";
import { avatarSeed } from "@/lib/botAvatar";
import { useChat } from "@/stores/chat";
import { useLLM } from "@/stores/llm";
import { initialForm, toInput, validate } from "./botForm";
import { Block, ExecutiveCard } from "./controls";
import { BuilderMenu } from "./BuilderMenu";
import { ModelPicker } from "./ModelPicker";
import { useModelOptions, type Option } from "./modelOptions";


/**
 * Creating a bot takes a name (which generates its avatar) and a model. The bot then interviews the user in its
 * own conversation and writes its own instructions; everything else lives in its settings panel.
 */
export function NewBot() {
  const [name, setName] = useState("");
  const [model, setModel] = useState("");
  const [executive, setExecutive] = useState(false);
  const [attempted, setAttempted] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Every bot gets an explicit model: the one picked, else the default chat model from AI settings.
  const defaultModel = useLLM((s) => s.defaults?.chatModel ?? "");
  const chosen = model || defaultModel;
  const models = useModelOptions(chosen);

  const form = { ...initialForm(), name, model: chosen, avatar: avatarSeed(name) };
  const nameProblem = validate(form).name;

  async function create() {
    setAttempted(true);
    if (saving || nameProblem) return;
    setSaving(true);
    setError(null);
    try {
      const input = toInput(form, "");
      const saved = await Chat.saveBot({ ...input, config: { ...input.config, setup: true, ...(executive ? { executive: true } : {}) } });
      useChat.getState().open(saved.id);
      useChat.getState().closeBuilder();
    } catch (cause) {
      setError(errText(cause));
      setSaving(false);
    }
  }

  const pick = (option: Option | null) => { if (option) setModel(option.value); };

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex items-center gap-2 border-b px-3 py-2">
        <Button size="icon-sm" variant="ghost" aria-label="Close the bot builder" onClick={() => useChat.getState().closeBuilder()}><X /></Button>
        <h1 className="min-w-0 flex-1 font-medium text-sm">New bot</h1>
        <BuilderMenu dirty={name.trim() !== ""} disabled={saving} />
      </header>
      <div className="flex min-h-0 flex-1 overflow-y-auto p-6">
        <form className="m-auto flex w-full max-w-sm flex-col gap-5" onSubmit={(event) => { event.preventDefault(); void create(); }}>
          <div className="flex flex-col items-center gap-2">
            <BotAvatar name={name} avatar={avatarSeed(name)} className="size-24" />
            <p className="text-muted-foreground text-xs">The avatar follows the name.</p>
          </div>
          {error && (
            <Alert variant="error">
              <AlertDescription className="selectable break-words">{error}</AlertDescription>
            </Alert>
          )}
          <Block label="Name" error={attempted ? nameProblem : undefined}>
            {(id, describedBy) => (
              <Input nativeInput id={id} autoFocus value={name} maxLength={160} placeholder="Travel planner" autoComplete="off" aria-required="true" aria-invalid={(attempted && !!nameProblem) || undefined} aria-describedby={describedBy} onChange={(e) => setName(e.target.value)} />
            )}
          </Block>
          <Block label="Model">
            {(id, describedBy) => <ModelPicker id={id} describedBy={describedBy} models={models} onPick={pick} />}
          </Block>
          <ExecutiveCard checked={executive} onChange={setExecutive} />
          <Button type="submit" disabled={saving}>{saving ? "Creating…" : "Create bot"}</Button>
        </form>
      </div>
    </div>
  );
}
