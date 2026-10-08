// Orole OS targets Windows: Ctrl stands where Shipyard's Mac app uses ⌘.
// Adapted from cosscom/shipyard app/src/lib/platform.ts (MIT), see docs/research/upstream-attribution.md.
export const IS_MAC = false;
const NAMES: Record<string, string> = { "⌃": "Ctrl", "⌘": "Ctrl", "⌥": "Alt", "⇧": "Shift" };
const ORDER = ["Ctrl", "Alt", "Shift"];
const KEYS: Record<string, string> = { "↵": "Enter", "⇥": "Tab", "⌫": "Backspace" };

export function platformKeys(s: string, mac = IS_MAC): string {
  if (mac || !/[⌃⌘⌥⇧]/.test(s)) return s;
  return s
    .replace(/([⌃⌘⌥⇧]+)-click/g, (_, m: string) => `${names(m).join("+")}-click`)
    .replace(/([⌃⌘⌥⇧]+) ?([^\s⌃⌘⌥⇧]+)/g, (_, m: string, key: string) => [...names(m), KEYS[key] ?? key].join("+"));
}
function names(mods: string): string[] {
  const out = new Set([...mods].map((c) => NAMES[c]).filter(Boolean));
  return ORDER.filter((n) => out.has(n));
}
