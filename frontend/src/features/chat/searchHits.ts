import type { SearchHit } from "@/components/assistant-ui/elements/conversation-search";
import type { Message } from "@/mygo";

const CONTEXT = 24;

/** What the reader sees of a message as text: visible text parts only, never reasoning, tools or hidden kickoff text. */
const searchableText = (message: Message): string =>
  message.parts.filter((p) => p.type === "text" && !p.hidden && p.text).map((p) => p.text).join(" ");

/** Every occurrence of `query` in the loaded messages, in thread order, with its surrounding words and its place on the track. */
export function findHits(messages: readonly Message[], query: string): SearchHit[] {
  if (query === "") return [];
  const needle = query.toLowerCase();
  const hits: SearchHit[] = [];
  messages.forEach((message, index) => {
    const text = searchableText(message);
    const haystack = text.toLowerCase();
    let from = 0;
    for (let at = haystack.indexOf(needle, from); at !== -1; at = haystack.indexOf(needle, from)) {
      hits.push({
        id: `${message.id}:${at}`,
        before: text.slice(Math.max(0, at - CONTEXT), at),
        match: text.slice(at, at + needle.length),
        after: text.slice(at + needle.length, at + needle.length + CONTEXT),
        position: (index / Math.max(1, messages.length - 1)) * 100,
      });
      from = at + needle.length;
    }
  });
  return hits;
}

/** The message a hit id belongs to. */
export const messageOf = (hitId: string) => hitId.slice(0, hitId.lastIndexOf(":"));
