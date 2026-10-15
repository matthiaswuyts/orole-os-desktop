import { PlayIcon, SquareIcon } from "lucide-react";
import { useEffect, useState, type SyntheticEvent } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
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

function Picker({ id, value, options, groups, caption, onChange, compact, className }: PickerLook & { options: Option[]; groups?: OptionGroup[]; caption?: string }) {
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
      {caption && <div className="px-2 pt-1.5 pb-1 text-muted-foreground text-xs">{caption}</div>}
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

/** The select value of the "Custom voice ID" option; never stored (a NUL cannot be in a voice id). */
const CUSTOM = "\u0000custom";

/**
 * The voice select for one call language: ElevenLabs voices fetched from the daemon for `language` (refetched when it
 * changes), grouped (Dutch by accent, English "Best for calls"), with a "Voices for" caption in the list. Choosing a
 * Voice Library voice asks the daemon to make it usable first (it adds it to the owner's ElevenLabs library when needed)
 * and stores the id the daemon returns. `allowCustom` adds a "Custom voice ID" choice that reveals a field for any
 * ElevenLabs voice id; a stored id the lists do not know opens in that mode.
 */
export function LanguageVoicePicker({ language, enabled, defaultId, allowCustom, ...look }: PickerLook & {
  language: string; enabled: boolean; defaultId: string; allowCustom?: boolean;
}) {
  const [list, setList] = useState<VoiceList | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  // Set once the person picks "Custom voice ID" or types in its field; until then a stored unknown id implies it.
  const [customDraft, setCustomDraft] = useState<string | null>(null);

  useEffect(() => {
    if (!inDesktop || !enabled) return;
    let live = true;
    Jarvis.voices(language).then((next) => { if (live) { setList(next); setError(null); } }, (failure: unknown) => { if (live) setError(`Could not load voices: ${errText(failure)}`); });
    return () => { live = false; };
  }, [language, enabled]);

  const known = knownVoices(list);
  const shown = list?.language ?? (language === "en" ? "en" : "nl");
  const custom = !!allowCustom && (customDraft !== null || (!!look.value && list !== null && !known.has(look.value)));
  const grouped = groupedVoiceOptions(list, look.value, defaultId);
  // In custom mode the stored id lives in the field, not as a raw-id option in the list.
  const top = custom ? grouped.top.filter((o) => o.value !== look.value || known.has(o.value)) : grouped.top;
  const groups = allowCustom
    ? [...grouped.groups, { label: "Other", options: [{ value: CUSTOM, title: "Custom voice ID", description: "Any voice in your ElevenLabs account, by its ID." }] }]
    : grouped.groups;

  async function choose(next: string) {
    if (next === CUSTOM) {
      setCustomDraft(known.has(look.value) ? "" : look.value);
      return;
    }
    setCustomDraft(null);
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

  return <span className="flex min-w-0 flex-col items-end gap-1">
    <Picker {...look} value={custom ? CUSTOM : look.value} options={top} groups={groups} caption={`Voices for: ${voiceLanguageName(shown)}`}
      onChange={(next) => void choose(next)} />
    {custom && <Input size="sm" className="w-52 font-mono" aria-label="ElevenLabs voice ID" placeholder="Paste a voice ID" spellCheck={false} autoComplete="off"
      value={customDraft ?? look.value} onChange={(event) => {
        const id = event.target.value.replace(/\s+/g, "");
        setCustomDraft(id);
        look.onChange(id);
      }} />}
    {adding && <span className="px-1 text-muted-foreground text-xs">Adding voice…</span>}
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
