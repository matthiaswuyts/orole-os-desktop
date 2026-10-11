import type { TurnEntry } from "@/lib/taskEvents";

/** One tick of the conversation map: a user message and everything the agent did and said in answer. */
export interface MapTurn {
  /** The scroll-anchor key of the turn's first row, which the map scrolls to. */
  id: string;
  title: string;
  preview: string;
  /** Entries in the turn, plus the steps and answers inside its runs. */
  length: number;
}

const flat = (text: string, max: number) => {
  const line = text.replace(/\s+/g, " ").trim();
  return line.length > max ? `${line.slice(0, max - 1)}…` : line;
};

/**
 * Splits a conversation into turns at user messages. `turnOfEntry[i]` is the turn that entry `i` belongs to.
 * Activity before the first user message (a loaded window that starts mid-turn) forms a turn of its own.
 */
export function mapTurns(entries: readonly TurnEntry[]): { turns: MapTurn[]; turnOfEntry: number[] } {
  const turns: MapTurn[] = [];
  const turnOfEntry: number[] = [];
  for (const entry of entries) {
    const user = entry.type === "item" && entry.item.type === "user" ? entry.item : null;
    if (user || turns.length === 0) {
      turns.push({ id: user ? user.key : entry.type === "item" ? entry.item.key : entry.key, title: user ? flat(user.text, 90) || "Message" : "Earlier in this conversation", preview: "", length: 0 });
    }
    const turn = turns[turns.length - 1]!;
    turnOfEntry.push(turns.length - 1);
    turn.length += entry.type === "run" ? 1 + entry.steps.length + entry.answer.length : 1;
    if (!turn.preview) {
      const candidates = entry.type === "item" ? [entry.item] : [...entry.answer, ...entry.steps];
      const reply = candidates.find((item) => item.type === "assistant");
      if (reply?.type === "assistant") turn.preview = flat(reply.text, 180);
    }
  }
  return { turns, turnOfEntry };
}
