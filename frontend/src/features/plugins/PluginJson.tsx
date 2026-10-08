/** Scrollable inspection is keyboard reachable; invisible formatting stays visible as JSON escapes. */
export function PluginJson({ value, label }: { value: unknown; label: string }) {
  const text = JSON.stringify(value, null, 2)?.replace(/\p{Cf}/gu, (character) => {
    const code = character.codePointAt(0)!;
    if (code <= 0xffff) return `\\u${code.toString(16).padStart(4, "0")}`;
    const surrogate = code - 0x10000;
    return `\\u${(0xd800 + (surrogate >> 10)).toString(16)}\\u${(0xdc00 + (surrogate & 0x3ff)).toString(16)}`;
  });
  return <pre tabIndex={0} aria-label={label} className="selectable max-h-80 overflow-auto whitespace-pre-wrap break-words rounded-md bg-muted p-3 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring">{text}</pre>;
}
