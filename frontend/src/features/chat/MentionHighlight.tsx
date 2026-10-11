import type { ReactNode, Ref } from "react";
import { BotAvatar } from "@/components/shell/BotAvatar";
import { mentionSpans, type MentionTarget } from "./mentions";

/**
 * The visible text of the composer. A textarea cannot style part of its text, so the textarea above this layer is
 * transparent (caret and selection still work) and this layer, which has the same box, font and wrapping, draws the text
 * with each @bot and /skill as a chip. A chip never changes the width of the characters it covers, so the caret cannot
 * drift: its padding is cancelled by an equal negative margin, a bot's `@` is replaced by its avatar painted over the
 * same slot, and a skill keeps its visible `/`. The caller keeps `scrollTop` in step with the textarea.
 */
export function MentionHighlight({
  text,
  targets,
  mirrorRef,
}: {
  text: string;
  targets: readonly MentionTarget[];
  mirrorRef: Ref<HTMLDivElement>;
}) {
  const parts: ReactNode[] = [];
  let at = 0;
  let prevEnd = 0;
  for (const { start, end, target } of mentionSpans(text, targets)) {
    if (start > at) parts.push(text.slice(at, start));
    parts.push(
      <mark
        key={start}
        data-slot="directive-text-chip"
        data-directive-type={target.type}
        data-directive-id={target.id}
        // Right after another chip only a space separates them, so this one gives up its left padding to leave a gap.
        className={`${start - prevEnd <= 1 && prevEnd > 0 ? "" : "-ml-1 pl-1 "}-mr-0.5 rounded-sm bg-secondary py-px pr-0.5 text-foreground [box-decoration-break:clone]`}
      >
        {target.type === "bot" ? (
          <span className="relative inline-block text-transparent">
            <span className="absolute inset-0 flex items-center justify-center">
              <BotAvatar
                name={target.label}
                avatar={target.avatar}
                className="size-3.5 text-[8px]"
              />
            </span>
            {text[start]}
          </span>
        ) : (
          <span className="text-muted-foreground">{text[start]}</span>
        )}
        {text.slice(start + 1, end)}
      </mark>,
    );
    at = end;
    prevEnd = end;
  }
  parts.push(text.slice(at), "\n"); // a trailing newline keeps the last empty line as tall as the textarea's
  return (
    <div
      ref={mirrorRef}
      aria-hidden
      data-slot="mention-highlight"
      className="pointer-events-none absolute inset-0 overflow-hidden px-1 py-2 text-sm break-words whitespace-pre-wrap text-foreground"
    >
      {parts}
    </div>
  );
}
