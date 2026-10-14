import { useEffect, useRef, type CSSProperties, type ReactNode } from "react";
import { menuPopup } from "@/components/assistant-ui/elements/surfaces";
import { cn } from "@/lib/utils";

/** An open `@` or `/` in a composer: the trigger character at `start` and what was typed after it, up to the caret. */
export interface MenuQuery { trigger: "@" | "/"; start: number; query: string }

/** How far back from the caret a trigger is looked for: longer than any name worth typing. */
const MAX_QUERY = 48;

/**
 * The `@` or `/` the caret is typing after, if any. The trigger starts the text or follows whitespace (so a path like
 * `src/app` or a mail address never opens the menu), and the query stays on its line and does not start with a space.
 * A query may hold later spaces ("@Research B"), so names of more than one word stay findable.
 */
export function menuQuery(text: string, caret: number): MenuQuery | null {
  const lineStart = text.lastIndexOf("\n", caret - 1) + 1;
  for (let i = caret - 1; i >= lineStart && caret - i <= MAX_QUERY; i--) {
    const char = text[i];
    if (char !== "@" && char !== "/") continue;
    if (i > 0 && !/\s/.test(text[i - 1]!)) continue;
    const query = text.slice(i + 1, caret);
    return /^\s/.test(query) ? null : { trigger: char, start: i, query };
  }
  return null;
}

/**
 * Items whose label matches `query`, best first: the label starts with it, then a word in it does, then it contains it.
 * Case is ignored; equal matches keep their order.
 */
export function matchItems<T extends { label: string }>(items: readonly T[], query: string): T[] {
  const q = query.toLowerCase();
  if (!q) return [...items];
  const ranked: { item: T; rank: number }[] = [];
  for (const item of items) {
    const label = item.label.toLowerCase();
    const rank = label.startsWith(q) ? 0 : label.split(/[\s_-]+/).some((word) => word.startsWith(q)) ? 1 : label.includes(q) ? 2 : -1;
    if (rank >= 0) ranked.push({ item, rank });
  }
  return ranked.sort((a, b) => a.rank - b.rank).map(({ item }) => item);
}

/** One row of the menu. Rows of a group are contiguous; the group's title is drawn above its first row. */
export interface MenuOption {
  key: string;
  group: string;
  label: string;
  detail: string;
  icon: ReactNode;
}

/**
 * The menu above a composer for `@` mentions and `/` commands: titled groups of rows with an icon, a name and one line
 * about it, the highlighted row marked with ↵. Focus stays in the composer's textarea (a combobox that owns `id`); it
 * moves the highlight with the arrow keys and picks with Enter or Tab, and the mouse highlights on hover and picks on click.
 * `columns` lines the details up after a fixed name column, as a command list reads best.
 */
export function ComposerMenu({ id, label, options, active, trigger, empty, columns = false, onActive, onPick }: {
  id: string;
  label: string;
  options: readonly MenuOption[];
  active: number;
  /** drawn before each name, muted */
  trigger: string;
  /** what an empty list says */
  empty: string;
  columns?: boolean;
  onActive: (index: number) => void;
  onPick: (index: number) => void;
}) {
  const list = useRef<HTMLDivElement>(null);
  useEffect(() => {
    list.current?.querySelector(`#${CSS.escape(`${id}-option-${active}`)}`)?.scrollIntoView({ block: "nearest" });
  }, [id, active]);
  const nameWidth = Math.min(18, Math.max(8, ...options.map((option) => option.label.length + 2)));
  return <div ref={list} id={id} role="listbox" aria-label={label} tabIndex={-1} data-slot="composer-menu"
    style={{ "--menu-name": `${nameWidth}ch` } as CSSProperties}
    className={cn(menuPopup, "absolute inset-x-0 bottom-[calc(100%+0.5rem)] z-50 max-h-[min(420px,50vh)] origin-bottom overflow-y-auto overscroll-contain p-1.5 duration-150 ease-out animate-in fade-in-0 zoom-in-[0.98] slide-in-from-bottom-1 motion-reduce:animate-none")}>
    {options.length === 0 && <div className="px-2 py-1.5 text-[13px] leading-5 text-muted-foreground">{empty}</div>}
    {options.map((option, index) => <div key={option.key} role="presentation" className="contents">
      {option.group !== options[index - 1]?.group && <div aria-hidden className="px-2 pt-2 pb-1 text-[11px] leading-4 font-medium text-muted-foreground first:pt-1">{option.group}</div>}
      <div id={`${id}-option-${index}`} role="option" aria-selected={index === active} data-slot="composer-menu-item"
        onMouseDown={(event) => event.preventDefault()} onMouseMove={() => { if (index !== active) onActive(index); }} onClick={() => onPick(index)}
        className={cn("flex h-8 w-full min-w-0 cursor-pointer items-center gap-2 rounded-[10px] px-2 text-[13px] leading-5 text-popover-foreground", index === active && "bg-foreground/6")}>
        <span aria-hidden className="flex size-[18px] flex-none items-center justify-center text-muted-foreground [&_svg]:size-4">{option.icon}</span>
        <span className={cn("truncate font-medium", columns ? "w-(--menu-name) flex-none" : "min-w-0 shrink-0 max-w-[60%]")}>
          <span className="text-muted-foreground">{trigger}</span>{option.label}
        </span>
        <span className="min-w-0 flex-1 truncate text-muted-foreground">{option.detail}</span>
        <kbd aria-hidden className={cn("inline-flex size-4.5 flex-none items-center justify-center rounded-[4px] bg-foreground/6 font-sans text-[10px] leading-none font-medium text-muted-foreground", index !== active && "invisible")}>↵</kbd>
      </div>
    </div>)}
  </div>;
}
