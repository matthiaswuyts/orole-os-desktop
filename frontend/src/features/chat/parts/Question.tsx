import { useState } from "react";
import type { Choice } from "@/mygo";
import { bubble } from "@/components/assistant-ui/elements/surfaces";
import { cn } from "@/lib/utils";

/**
 * A question from the bot: lettered suggestions (label and optional one-line description) plus a field for the
 * user's own answer. Any answer goes out as the user's next message. Without `onAnswer` (already answered, or the
 * bot is still replying) the card is shown read-only.
 */
export function QuestionCard({ text, choices, onAnswer }: { text: string; choices: readonly Choice[]; onAnswer?: (answer: string) => void }) {
  const [own, setOwn] = useState("");
  const answer = (value: string) => {
    const trimmed = value.trim();
    if (trimmed && onAnswer) onAnswer(trimmed);
  };
  return <fieldset data-slot="chat-question" disabled={!onAnswer} className={cn(bubble, "flex min-w-0 flex-col gap-3 p-3 disabled:opacity-60")}>
    <legend className="sr-only">Question from the bot</legend>
    <div className="flex flex-col gap-0.5 px-1">
      <p className="selectable font-medium text-sm">{text}</p>
      <p className="text-muted-foreground text-xs">Pick one or just tell me in your own words.</p>
    </div>
    <ul aria-label="Suggested answers" className="flex list-none flex-col divide-y overflow-hidden rounded-xl border bg-muted/40 p-0">
      {choices.map((choice, index) => <li key={`${index}-${choice.label}`}>
        <button type="button" onClick={() => answer(choice.label)}
          className="flex w-full items-center gap-3 px-3 py-2 text-left outline-none transition-colors enabled:hover:bg-accent focus-visible:bg-accent">
          <kbd aria-hidden className="grid size-6 shrink-0 place-items-center rounded-md bg-muted font-medium text-muted-foreground text-xs">{String.fromCharCode(65 + index)}</kbd>
          <span className="flex min-w-0 flex-col">
            <span className="font-medium text-sm">{choice.label}</span>
            {choice.description && <span className="text-muted-foreground text-xs">{choice.description}</span>}
          </span>
        </button>
      </li>)}
    </ul>
    <form onSubmit={(event) => { event.preventDefault(); answer(own); setOwn(""); }}>
      <input aria-label="Type your own answer" placeholder="Type your own answer" value={own} maxLength={500} autoComplete="off"
        onChange={(event) => setOwn(event.target.value)}
        className="h-10 w-full rounded-xl border bg-background px-3 text-sm outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/24 disabled:cursor-not-allowed" />
    </form>
  </fieldset>;
}
