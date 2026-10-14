import { Badge } from "@/components/ui/badge";
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";
import type { TTSModel, Voice } from "@/mygo";

/** One choice in a picker. `value` "" means "use the default". */
interface Option {
  value: string;
  title: string;
  description?: string;
  recommended?: boolean;
  /** Seed for the avatar; omitted for options without one. */
  avatar?: string;
}

function hash(text: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < text.length; i++) {
    h ^= text.charCodeAt(i);
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return h;
}

/** A deterministic two-hue gradient for a voice id: the same id always gives the same circle. */
export function voiceGradient(id: string): string {
  const h = hash(id);
  const first = h % 360;
  const second = (first + 40 + ((h >>> 9) % 120)) % 360;
  return `linear-gradient(135deg, hsl(${first} 70% 62%), hsl(${second} 65% 46%))`;
}

export function VoiceAvatar({ id, size }: { id: string; size: "sm" | "md" }) {
  return <span aria-hidden="true" data-slot="voice-avatar" style={{ backgroundImage: voiceGradient(id) }}
    className={cn("inline-block shrink-0 rounded-full", size === "md" ? "size-7" : "size-5")} />;
}

function OptionBody({ option }: { option: Option }) {
  return <span className="flex min-w-0 items-center gap-2">
    {option.avatar && <VoiceAvatar id={option.avatar} size="md" />}
    <span className="min-w-0">
      <span className="flex items-center gap-1.5">
        <span className="font-medium">{option.title}</span>
        {option.recommended && <Badge variant="success" size="sm">Recommended</Badge>}
      </span>
      {option.description && <span className="line-clamp-2 block text-muted-foreground text-xs">{option.description}</span>}
    </span>
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

function Picker({ id, value, options, onChange, compact, className }: PickerLook & { options: Option[] }) {
  const byValue = new Map(options.map((o) => [o.value, o]));
  return <Select value={value} items={options.map((o) => ({ value: o.value, label: o.title }))} onValueChange={(next: string | null) => onChange(next ?? "")}>
    <SelectTrigger id={id} size="sm" className={cn(compact ? "w-auto min-w-24 max-w-52 border-0 bg-transparent shadow-none" : "w-52", className)}>
      <SelectValue>{(current: string | null) => { const o = byValue.get(current ?? ""); return o ? <TriggerBody option={o} /> : null; }}</SelectValue>
    </SelectTrigger>
    <SelectPopup alignItemWithTrigger={false} className="max-h-80">
      {options.map((o) => <SelectItem key={o.value} value={o.value}><OptionBody option={o} /></SelectItem>)}
    </SelectPopup>
  </Select>;
}

/** Voice choices: avatar, name and description. `defaultId` is the global default, named in the first option. */
export function voiceOptions(voices: Voice[], value: string, defaultId: string): Option[] {
  const fallback = voices.find((v) => v.id === defaultId);
  const options: Option[] = [
    { value: "", title: fallback ? `Default (${fallback.name})` : "Default", description: fallback?.description, avatar: fallback?.id },
    ...voices.map((v) => ({ value: v.id, title: v.name, description: v.description, avatar: v.id })),
  ];
  if (value && !options.some((o) => o.value === value)) options.push({ value, title: value });
  return options;
}

/** Model choices; a stored model outside the curated list stays selectable as "<id> (not recommended)". */
export function modelOptions(models: TTSModel[], value: string, defaultId: string): Option[] {
  const fallback = models.find((m) => m.id === defaultId);
  const options: Option[] = [
    { value: "", title: fallback ? `Default (${fallback.name})` : "Default", description: fallback?.description },
    ...models.map((m) => ({ value: m.id, title: m.name, description: m.description, recommended: m.recommended })),
  ];
  if (value && !options.some((o) => o.value === value)) options.push({ value, title: `${value} (not recommended)` });
  return options;
}

export function VoicePicker({ voices, defaultId, ...look }: PickerLook & { voices: Voice[]; defaultId: string }) {
  return <Picker {...look} options={voiceOptions(voices, look.value, defaultId)} />;
}

export function ModelPicker({ models, defaultId, ...look }: PickerLook & { models: TTSModel[]; defaultId: string }) {
  return <Picker {...look} options={modelOptions(models, look.value, defaultId)} />;
}
