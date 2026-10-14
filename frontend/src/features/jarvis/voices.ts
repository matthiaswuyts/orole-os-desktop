import type { TTSModel, Voice, VoiceList } from "@/mygo";

/** One choice in a picker. `value` "" means "use the default". */
export interface Option {
  value: string;
  title: string;
  description?: string;
  recommended?: boolean;
  /** Seed for the avatar; omitted for options without one. */
  avatar?: string;
  /** A public audio clip of the voice, for the preview button. */
  preview?: string;
}

/** A titled block of options in a picker. */
export interface OptionGroup {
  label: string;
  options: Option[];
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

/** Voice choices: avatar, name and description. `defaultId` is the global default, named in the first option. */
export function voiceOptions(voices: Voice[], value: string, defaultId: string): Option[] {
  const fallback = voices.find((v) => v.id === defaultId);
  const options: Option[] = [
    { value: "", title: fallback ? `Default (${fallback.name})` : "Default", description: fallback?.description, avatar: fallback?.id, preview: fallback?.previewUrl || undefined },
    ...voices.map((v) => ({ value: v.id, title: v.name, description: v.description, avatar: v.id, preview: v.previewUrl || undefined })),
  ];
  if (value && !options.some((o) => o.value === value)) options.push({ value, title: value });
  return options;
}

/** A voice's description line: ElevenLabs' text, else its accent, gender and age. */
function describeVoice(v: Voice): string {
  if (v.description) return v.description;
  return [v.accent, v.gender, v.age].filter(Boolean).join(" · ");
}

function voiceOption(v: Voice): Option {
  return { value: v.id, title: v.name, description: describeVoice(v), avatar: v.id, preview: v.previewUrl || undefined };
}

/** Every voice a list knows: the grouped ones and the account's own library. */
export function knownVoices(list: VoiceList | null): Map<string, Voice> {
  const known = new Map<string, Voice>();
  for (const v of list?.library ?? []) known.set(v.id, v);
  for (const g of list?.groups ?? []) for (const v of g.voices) if (!known.has(v.id)) known.set(v.id, v);
  return known;
}

/**
 * Grouped voice choices for one call language. The "Default (<name>)" option comes first; a stored voice that is not
 * in the groups stays selectable under its library name (or its id) so the select still shows it.
 */
export function groupedVoiceOptions(list: VoiceList | null, value: string, defaultId: string): { top: Option[]; groups: OptionGroup[] } {
  const known = knownVoices(list);
  const fallback = known.get(defaultId);
  const top: Option[] = [{ value: "", title: fallback ? `Default (${fallback.name})` : "Default", description: fallback ? describeVoice(fallback) : undefined,
    avatar: fallback?.id, preview: fallback?.previewUrl || undefined }];
  const groups = (list?.groups ?? []).filter((g) => g.voices.length > 0).map((g) => ({ label: g.label, options: g.voices.map(voiceOption) }));
  if (value && !groups.some((g) => g.options.some((o) => o.value === value))) {
    const stored = known.get(value);
    top.push(stored ? voiceOption(stored) : { value, title: value });
  }
  return { top, groups };
}

/** The name of a voice list's language for the "Voices for" note. */
export function voiceLanguageName(language: string): string {
  return language === "en" ? "English" : "Nederlands";
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

/** The call languages of Settings › Phone (PhoneSettings.callLanguage; "" is stored as Dutch). */
export const CALL_LANGUAGES: Option[] = [
  { value: "nl", title: "Nederlands", description: "Calls are in Dutch" },
  { value: "en", title: "English", description: "Calls are in English" },
  { value: "auto", title: "Auto", description: "Starts in Dutch and switches if you speak another language" },
];
