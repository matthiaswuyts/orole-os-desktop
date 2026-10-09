import { Avatar, Style } from "@dicebear/core";
import definition from "@dicebear/styles/gaze.json" with { type: "json" };

// Gaze by DiceBear, licensed under CC0 1.0.
const style = new Style(definition);
const cache = new Map<string, string>();

/** The seed stored in Bot.avatar: a short stable token derived from the bot's name when it is created. */
export function avatarSeed(name: string): string {
  let hash = 2166136261;
  for (const char of name.trim().toLocaleLowerCase()) hash = Math.imul(hash ^ char.codePointAt(0)!, 16777619) >>> 0;
  return hash.toString(36).padStart(7, "0");
}

/** A data URL of the generated avatar for a seed (rendered through <img>, so it can never run script). */
export function avatarUrl(seed: string): string {
  let url = cache.get(seed);
  if (url === undefined) {
    const svg = new Avatar(style, { tags: ["animation"], seed }).toString();
    url = `data:image/svg+xml;utf8,${encodeURIComponent(svg)}`;
    if (cache.size >= 500) cache.clear();
    cache.set(seed, url);
  }
  return url;
}
