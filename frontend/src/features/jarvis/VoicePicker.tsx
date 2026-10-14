import { PlayIcon, SquareIcon } from "lucide-react";
import { useEffect, useState, type SyntheticEvent } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select, SelectGroup, SelectGroupLabel, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { errText } from "@/features/agents/format";
import { inDesktop } from "@/lib/backend";
import { cn } from "@/lib/utils";
import { Jarvis, type TTSModel, type Voice, type VoiceList } from "@/mygo";
import { stopPreview, togglePreview, usePreviewPlaying } from "./preview";
import { CALL_LANGUAGES, groupedVoiceOptions, knownVoices, modelOptions, voiceGradient, voiceLanguageName, voiceOptions, type Option, type OptionGroup } from "./voices";

export function VoiceAvatar({ id, size }: { id: string; size: "sm" | "md" }) {
  return <span aria-hidden="true" data-slot="voice-avatar" style={{ backgroundImage: voiceGradient(id) }}
    className={cn("inline-block shrink-0 rounded-full", size === "md" ? "size-7" : "size-5")} />;
}

/** Keeps an event inside the preview button: the select item around it must not select or close. */
function contain(event: SyntheticEvent) {
  event.stopPropagation();
}

/** Plays or stops a voice's public preview clip. Inside a select item it never selects the item. */
export function PreviewButton({ url, name }: { url: string; name: string }) {
  const playing = usePreviewPlaying(url);
  return <Tooltip>
    <TooltipTrigger render={<Button type="button" variant="ghost" size="icon-xs" aria-label={`Preview ${name}`} aria-pressed={playing}
      onPointerDown={(event) => { contain(event); event.preventDefault(); }} onPointerUp={contain} onMouseDown={contain} onMouseUp={contain}
      onKeyDown={(event) => {
        contain(event);
        if (event.key === "Enter" || event.key === " ") { event.preventDefault(); togglePreview(url); }
      }}
      onKeyUp={contain}
      onClick={(event) => { contain(event); event.preventDefault(); togglePreview(url); }} />}>
      {playing ? <SquareIcon aria-hidden /> : <PlayIcon aria-hidden />}
    </TooltipTrigger>
    <TooltipPopup>{playing ? "Stop" : "Preview"}</TooltipPopup>
  </Tooltip>;
}

function OptionBody({ option }: { option: Option }) {
  return <span className="flex w-full min-w-0 items-center gap-2">
    {option.avatar && <VoiceAvatar id={option.avatar} size="md" />}
    <span className="min-w-0 flex-1">
      <span className="flex items-center gap-1.5">
        <span className="font-medium">{option.title}</span>
        {option.recommended && <Badge variant="success" size="sm">Recommended</Badge>}
      </span>
      {option.description && <span className="line-clamp-2 block text-muted-foreground text-xs">{option.description}</span>}
    </span>
    {option.preview && <PreviewButton url={option.preview} name={option.title} />}
  </span>;
}

function TriggerBody({ option }: { option: Option }) {
  return <span className="flex min-w-0 items-center gap-1.5">
    {option.avatar && <VoiceAvatar id={option.avatar} size="sm" />}
    <span className="truncate">{option.title}</span>
  </span>;
}

interface PickerLook {
  id: string;
  value: string;
  onChange: (value: string) => void;
  /** A compact, borderless trigger for use inside a row. */
  compact?: boolean;
  className?: string;
}

function Picker({ id, value, options, groups, onChange, compact, className }: PickerLook & { options: Option[]; groups?: OptionGroup[] }) {
  const all = [...options, ...(groups ?? []).flatMap((g) => g.options)];
  const byValue = new Map(all.map((o) => [o.value, o]));
  const hasPreview = all.some((o) => o.preview);
  useEffect(() => (hasPreview ? stopPreview : undefined), [hasPreview]);
  return <Select value={value} items={all.map((o) => ({ value: o.value, label: o.title }))} onValueChange={(next: string | null) => onChange(next ?? "")}
    onOpenChange={(open: boolean) => { if (!open && hasPreview) stopPreview(); }}>
    <SelectTrigger id={id} size="sm" className={cn(compact ? "w-auto min-w-24 max-w-52 border-0 bg-transparent shadow-none" : "w-52", className)}>
      <SelectValue>{(current: string | null) => { const o = byValue.get(current ?? ""); return o ? <TriggerBody option={o} /> : null; }}</SelectValue>
    </SelectTrigger>
    <SelectPopup alignItemWithTrigger={false} align="end" className="max-h-80 w-80 max-w-[calc(100vw-2rem)]">
      {options.map((o) => <SelectItem key={o.value} value={o.value}><OptionBody option={o} /></SelectItem>)}
      {groups?.map((g) => <SelectGroup key={g.label}>
        <SelectGroupLabel>{g.label}</SelectGroupLabel>
        {g.options.map((o) => <SelectItem key={o.value} value={o.value}><OptionBody option={o} /></SelectItem>)}
      </SelectGroup>)}
    </SelectPopup>
  </Select>;
}

export function VoicePicker({ voices, defaultId, ...look }: PickerLook & { voices: Voice[]; defaultId: string }) {
  return <Picker {...look} options={voiceOptions(voices, look.value, defaultId)} />;
}

/**
 * The voice select for one call language: ElevenLabs voices fetched from the daemon for `language` (refetched when it
 * changes), grouped (Dutch by accent, English "Best for calls"), with a "Voices for" note. Choosing a Voice Library
 * voice asks the daemon to make it usable first (it adds it to the owner's ElevenLabs library when needed) and stores
 * the id the daemon returns. `previewSelected` adds a preview button for the chosen voice next to the select.
 */
export function LanguageVoicePicker({ language, enabled, defaultId, previewSelected, ...look }: PickerLook & {
  language: string; enabled: boolean; defaultId: string; previewSelected?: boolean;
}) {
  const [list, setList] = useState<VoiceList | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);

  useEffect(() => {
    if (!inDesktop || !enabled) return;
    let live = true;
    Jarvis.voices(language).then((next) => { if (live) { setList(next); setError(null); } }, (failure: unknown) => { if (live) setError(`Could not load voices: ${errText(failure)}`); });
    return () => { live = false; };
  }, [language, enabled]);

  const { top, groups } = groupedVoiceOptions(list, look.value, defaultId);
  const known = knownVoices(list);
  const shown = list?.language ?? (language === "en" ? "en" : "nl");

  async function choose(next: string) {
    const voice = known.get(next);
    if (!next || !voice || voice.inLibrary || !voice.publicOwnerId) {
      look.onChange(next);
      return;
    }
    setAdding(true);
    setError(null);
    try {
      look.onChange(await Jarvis.addVoice(voice.id, voice.publicOwnerId));
    } catch (failure) {
      setError(`Could not add ${voice.name} to your ElevenLabs voices: ${errText(failure)}`);
    } finally {
      setAdding(false);
    }
  }

  const selected = known.get(look.value || defaultId);
  return <span className="flex min-w-0 flex-col items-end gap-0.5">
    <span className="flex items-center gap-1">
      {previewSelected && selected?.previewUrl && <PreviewButton url={selected.previewUrl} name={selected.name} />}
      <Picker {...look} options={top} groups={groups} onChange={(next) => void choose(next)} />
    </span>
    <span className="px-1 text-muted-foreground text-xs">{adding ? "Adding voice…" : `Voices for: ${voiceLanguageName(shown)}`}</span>
    {error && <span role="alert" className="selectable max-w-80 break-words px-1 text-destructive-foreground text-xs">{error}</span>}
  </span>;
}

/** The language calls are held in. */
export function LanguagePicker(look: PickerLook) {
  return <Picker {...look} options={CALL_LANGUAGES} />;
}

export function ModelPicker({ models, defaultId, ...look }: PickerLook & { models: TTSModel[]; defaultId: string }) {
  return <Picker {...look} options={modelOptions(models, look.value, defaultId)} />;
}
