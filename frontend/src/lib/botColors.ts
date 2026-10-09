/** The fixed colour tokens a bot may use (Bot.color). Unknown values fall back to "slate". */
export const BOT_COLORS = ["blue", "violet", "pink", "red", "orange", "amber", "green", "teal", "slate"] as const;
export type BotColor = (typeof BOT_COLORS)[number];

// Literal class names so Tailwind can see them.
const TONE: Record<BotColor, string> = {
  blue: "bg-blue-500/20 text-blue-700 dark:text-blue-300",
  violet: "bg-violet-500/20 text-violet-700 dark:text-violet-300",
  pink: "bg-pink-500/20 text-pink-700 dark:text-pink-300",
  red: "bg-red-500/20 text-red-700 dark:text-red-300",
  orange: "bg-orange-500/20 text-orange-700 dark:text-orange-300",
  amber: "bg-amber-500/20 text-amber-700 dark:text-amber-300",
  green: "bg-green-500/20 text-green-700 dark:text-green-300",
  teal: "bg-teal-500/20 text-teal-700 dark:text-teal-300",
  slate: "bg-slate-500/20 text-slate-700 dark:text-slate-300",
};

export const isBotColor = (value: string): value is BotColor => value in TONE;

/** Background and text classes for a colour token. */
export const botTone = (color: string): string => TONE[isBotColor(color) ? color : "slate"];

/** Up to two initials from a name ("Travel planner" -> "TP"). */
export function initials(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean);
  const letters = words.length > 1 ? [words[0]!, words[1]!].map((w) => [...w][0]!) : [...(words[0] ?? "?")].slice(0, 2);
  return letters.join("").toUpperCase();
}

/** The glyph shown in an avatar: the bot's emoji/initials (at most two graphemes), else initials of its name. */
export function avatarGlyph(name: string, avatar: string): string {
  const own = avatar.trim();
  return own ? Array.from(new Intl.Segmenter().segment(own), (s) => s.segment).slice(0, 2).join("") : initials(name);
}
