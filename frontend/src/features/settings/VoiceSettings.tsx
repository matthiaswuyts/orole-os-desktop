import { useEffect, useState, type ReactNode } from "react";
import { DownloadIcon, GlobeIcon, Trash2Icon } from "lucide-react";
import type { ModelStatus, VoiceStatus } from "@/mygo";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { ConfirmDialog } from "@/features/projects/dialogs";
import { Failure, Loading } from "@/features/mcp/shared";
import { SettingsSection } from "@/features/settings/SettingsSection";
import { inDesktop } from "@/lib/backend";
import { megabytes, useDictation } from "@/stores/dictation";

/** One settings row: what it is on the left, its control on the right, anything that went wrong below. */
function Row({ label, labelId, hint, children, below }: { label: ReactNode; labelId?: string; hint?: ReactNode; children: ReactNode; below?: ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5 px-3 py-2.5">
      <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2">
        <div className="min-w-0 flex-1">
          <div id={labelId}>{label}</div>
          {hint && <div className="text-xs text-muted-foreground">{hint}</div>}
        </div>
        <div className="flex shrink-0 items-center gap-1.5">{children}</div>
      </div>
      {below}
    </div>
  );
}

function ModelRow({ model, status }: { model: ModelStatus; status: VoiceStatus }) {
  const busy = useDictation((state) => state.busy[model.id]);
  const actionError = useDictation((state) => state.actionError?.target === model.id ? state.actionError.message : null);
  const [confirming, setConfirming] = useState(false);
  const { download, cancel, remove, choose } = useDictation.getState();
  const inUse = status.model === model.id;
  const ready = model.state === "ready";
  const labelId = `voice-model-${model.id}`;

  let controls: ReactNode;
  if (model.state === "downloading") {
    const percent = model.totalBytes > 0 ? Math.min(100, (model.receivedBytes / model.totalBytes) * 100) : null;
    controls = <>
      <div className="flex w-44 flex-col gap-1">
        <Progress value={percent} aria-labelledby={labelId} />
        <span className="text-xs tabular-nums text-muted-foreground">{megabytes(model.receivedBytes)} of {megabytes(model.totalBytes || model.downloadBytes)} MB</span>
      </div>
      <Button size="sm" variant="outline" disabled={!!busy} onClick={() => void cancel(model.id)}>Cancel</Button>
    </>;
  } else if (ready) {
    controls = inUse
      ? <span className="text-xs text-muted-foreground">In use</span>
      : <Button size="sm" variant="outline" loading={busy === "use"} disabled={!!busy} onClick={() => void choose(model.id)}>Use</Button>;
  } else if (model.state === "failed") {
    controls = <Button size="sm" variant="outline" loading={busy === "download"} disabled={!!busy} onClick={() => void download(model.id)}>Retry</Button>;
  } else {
    controls = <Button size="sm" variant="outline" loading={busy === "download"} disabled={!!busy} onClick={() => void download(model.id)}><DownloadIcon aria-hidden />Download</Button>;
  }
  // A failed download may have left part of the files behind; removing deletes them too.
  const removable = ready || model.state === "failed";

  return (
    <Row labelId={labelId} label={<span className="font-medium">{model.name}</span>}
      hint={<>
        <p>{model.description}</p>
        {!ready && <p>Download size: {megabytes(model.downloadBytes)} MB</p>}
        {ready && <p>On this computer: {megabytes(model.sizeBytes)} MB</p>}
      </>}
      below={<>
        {model.state === "failed" && model.error && <p role="alert" className="selectable break-words text-xs text-destructive-foreground">{model.error}</p>}
        {actionError && !confirming && <p role="alert" className="selectable break-words text-xs text-destructive-foreground">{actionError}</p>}
      </>}>
      {controls}
      {removable && <Tooltip>
        <TooltipTrigger render={<Button size="icon-sm" variant="ghost" aria-label={`Remove ${model.name}`} disabled={!!busy} onClick={() => setConfirming(true)} />}><Trash2Icon aria-hidden /></TooltipTrigger>
        <TooltipPopup>Remove</TooltipPopup>
      </Tooltip>}
      <ConfirmDialog open={confirming} onOpenChange={setConfirming} title={`Remove ${model.name}?`} confirmLabel="Remove"
        onConfirm={async () => {
          const outcome = await remove(model.id);
          if (!outcome.ok) throw new Error(outcome.error);
        }}>
        {ready
          ? <p>This deletes the model from this computer and frees {megabytes(model.sizeBytes)} MB. You can download it again at any time.{inUse ? " Dictation uses another downloaded model, or is off until you download one." : ""}</p>
          : <p>This deletes what was downloaded so far. You can download it again at any time.</p>}
      </ConfirmDialog>
    </Row>
  );
}

function LanguageRow({ status }: { status: VoiceStatus }) {
  const busy = useDictation((state) => !!state.busy.language);
  const actionError = useDictation((state) => state.actionError?.target === "language" ? state.actionError.message : null);
  const current = status.models.find((model) => model.id === status.model && model.state === "ready");
  const chooser = status.models.find((model) => model.languageChoice);
  const enabled = !!current?.languageChoice;
  const items = [{ value: "", label: "Automatic" }, ...status.languages.map((language) => ({ value: language.code, label: language.name }))];
  let hint: string;
  if (enabled) hint = "Automatic detects it from what you say. Setting it helps with short recordings.";
  else if (current) hint = `${current.name} finds the spoken language itself.${chooser ? ` To set a language, use ${chooser.name}.` : ""}`;
  else hint = "Download a dictation model first.";

  return (
    <Row labelId="voice-language-label" label="Dictation language" hint={hint}
      below={actionError && <p role="alert" className="selectable break-words text-xs text-destructive-foreground">{actionError}</p>}>
      <Select value={enabled ? status.language : ""} items={items} disabled={!enabled || busy}
        onValueChange={(next: string | null) => { if (next !== null && next !== status.language) void useDictation.getState().setLanguage(next); }}>
        <SelectTrigger size="sm" aria-labelledby="voice-language-label" className="w-52">
          <GlobeIcon aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
          <SelectValue />
        </SelectTrigger>
        <SelectPopup alignItemWithTrigger={false} className="max-h-80">
          {items.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
        </SelectPopup>
      </Select>
    </Row>
  );
}

/** Settings > Voice: the local speech models dictation uses, their downloads, and the spoken language. */
export function VoiceSettings() {
  const status = useDictation((state) => state.status);
  const error = useDictation((state) => state.error);
  useEffect(() => {
    const stop = useDictation.getState().startSync();
    void useDictation.getState().load();
    return stop;
  }, []);

  if (!inDesktop) return <p role="status" className="rounded-lg border bg-card p-3 text-muted-foreground">Voice settings are available in the Orole OS desktop app.</p>;
  if (!status) return error ? <Failure error={error} retry={() => void useDictation.getState().load()} /> : <Loading>Loading voice settings…</Loading>;
  if (!status.supported) {
    return <p role="status" className="rounded-lg border bg-card p-3 text-muted-foreground">Dictation needs Windows on an x64 processor. The speech engine has no build for this computer, so there is nothing to download.</p>;
  }
  const licenses = status.models.map((model) => model.license).filter(Boolean);
  return (
    <div className="flex min-w-0 flex-col">
      {error && <div className="mb-3"><Failure error={error} retry={() => void useDictation.getState().load()} /></div>}
      <SettingsSection id="settings-voice-models" title="Dictation model" description="Dictation turns what you say into text in the message boxes. Download a model to use it.">
        {status.models.map((model) => <ModelRow key={model.id} model={model} status={status} />)}
      </SettingsSection>
      <SettingsSection id="settings-voice-language" title="Language">
        <LanguageRow status={status} />
      </SettingsSection>
      <p className="max-w-prose text-xs text-muted-foreground">
        Dictation runs on this computer. Recordings and their text never leave it and are not saved.
        {licenses.length > 0 && ` Models: ${licenses.join("; ")}.`} Runs with sherpa-onnx (Apache 2.0).
      </p>
    </div>
  );
}
