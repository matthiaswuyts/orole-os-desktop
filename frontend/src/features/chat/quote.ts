/** The longest passage quoted into a message; the rest of a long selection is cut. */
const MAX_QUOTE = 1500;

/** A message that quotes a passage: the passage as markdown quote lines, a blank line, then what is asked of it. */
export function withQuote(passage: string, ask: string): string {
  const clipped = passage.trim().length > MAX_QUOTE ? `${passage.trim().slice(0, MAX_QUOTE).trimEnd()}…` : passage.trim();
  const quoted = clipped.split(/\r?\n/).map((line) => (line ? `> ${line}` : ">")).join("\n");
  return `${quoted}\n\n${ask}`;
}

/** Splits a message that starts with quote lines into the passage and the rest; no quote = the text is all rest. */
export function splitQuote(text: string): { quote: string; rest: string } {
  const lines = text.split(/\r?\n/);
  let n = 0;
  while (n < lines.length && /^>( |$)/.test(lines[n]!)) n++;
  if (n === 0) return { quote: "", rest: text };
  const quote = lines.slice(0, n).map((l) => l.replace(/^> ?/, "")).join("\n").trim();
  return { quote, rest: lines.slice(n).join("\n").replace(/^\s*\n/, "").trim() };
}
