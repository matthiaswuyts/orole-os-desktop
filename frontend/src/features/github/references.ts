export type ReferenceToken = { kind: "text"; value: string } | { kind: "mention"; value: string } | { kind: "issue"; value: string };

// A reference must start the text or follow a non-word character, so emails, URLs and `a#1` stay text.
// `@org/team` is excluded: the slash makes it a team handle this view cannot link.
const REFERENCE = /(^|[^\w@/#&.])(@[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})(?![\w/-])|#\d+(?!\w))/g;

/** Splits plain text into text, `@login` mentions and `#123` issue references. */
export function splitReferences(text: string): ReferenceToken[] {
  const out: ReferenceToken[] = [];
  let last = 0;
  for (const match of text.matchAll(REFERENCE)) {
    const start = match.index + match[1]!.length;
    if (start > last) out.push({ kind: "text", value: text.slice(last, start) });
    const value = match[2]!;
    out.push({ kind: value.startsWith("@") ? "mention" : "issue", value });
    last = start + value.length;
  }
  if (last < text.length) out.push({ kind: "text", value: text.slice(last) });
  return out;
}
