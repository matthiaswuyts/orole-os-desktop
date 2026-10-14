import { Badge } from "@/components/ui/badge";
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";
import type { TTSModel, Voice } from "@/mygo";
import { modelOptions, voiceGradient, voiceOptions, type Option } from "./voices";

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
    <SelectPopup alignItemWithTrigger={false} align="end" className="max-h-80 w-80 max-w-[calc(100vw-2rem)]">
      {options.map((o) => <SelectItem key={o.value} value={o.value}><OptionBody option={o} /></SelectItem>)}
    </SelectPopup>
  </Select>;
}

export function VoicePicker({ voices, defaultId, ...look }: PickerLook & { voices: Voice[]; defaultId: string }) {
  return <Picker {...look} options={voiceOptions(voices, look.value, defaultId)} />;
}

export function ModelPicker({ models, defaultId, ...look }: PickerLook & { models: TTSModel[]; defaultId: string }) {
  return <Picker {...look} options={modelOptions(models, look.value, defaultId)} />;
}
