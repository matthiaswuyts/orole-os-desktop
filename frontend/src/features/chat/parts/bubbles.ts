const LIST_ITEM = /^\s*(?:[-*+]|\d+[.)])\s/;

/**
 * Splits a reply into the chat bubbles it is drawn as: one per paragraph, the way a colleague sends several short
 * messages instead of one long one. Stays whole what must not be cut: a fenced code block (its blank lines belong
 * to it), and a line ending in ":" together with the list or block it introduces. Pure and stable while text
 * streams in: earlier bubbles never change when later text arrives, except the last one growing.
 */
export function splitBubbles(text: string): string[] {
  const chunks: string[] = [];
  let current: string[] = [];
  let fence = "";
  const flush = () => {
    const chunk = current.join("\n").trim();
    if (chunk) chunks.push(chunk);
    current = [];
  };
  for (const line of text.replace(/\r\n?/g, "\n").split("\n")) {
    const marker = /^\s*(`{3,}|~{3,})/.exec(line)?.[1];
    if (marker) {
      if (!fence) fence = marker[0]!.repeat(marker.length);
      else if (marker.startsWith(fence[0]!) && marker.length >= fence.length) fence = "";
    }
    if (!fence && line.trim() === "" && !marker) {
      flush();
      continue;
    }
    current.push(line);
  }
  flush();

  const merged: string[] = [];
  for (const chunk of chunks) {
    const previous = merged[merged.length - 1];
    const introduced = previous !== undefined && previous.endsWith(":") && !previous.includes("\n\n");
    const continuesList = previous !== undefined && LIST_ITEM.test(chunk) && LIST_ITEM.test(previous.split("\n").at(-1) ?? "");
    if (introduced || continuesList) merged[merged.length - 1] = `${previous}\n\n${chunk}`;
    else merged.push(chunk);
  }
  return merged;
}
