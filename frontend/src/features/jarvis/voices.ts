import type { TTSModel, Voice } from "@/mygo";

/** One choice in a picker. `value` "" means "use the default". */
export interface Option {
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

/** The call languages of Settings › Phone (PhoneSettings.callLanguage; "" is stored as Dutch). */
export const CALL_LANGUAGES: Option[] = [
  { value: "nl", title: "Nederlands", description: "Calls are in Dutch" },
  { value: "en", title: "English", description: "Calls are in English" },
  { value: "auto", title: "Auto", description: "Starts in Dutch and switches if you speak another language" },
];
