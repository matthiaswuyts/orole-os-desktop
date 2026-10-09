import { useId, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { cn } from "@/lib/utils";
import { Textarea } from "@/components/ui/textarea";
import { activeTrigger, applyMention, suggestMentions, type IssueRef, type MentionOption } from "./mentions";

interface MentionTextareaProps {
  value: string;
  onChange: (value: string) => void;
  /** Ctrl/Cmd+Enter. */
  onSubmit: () => void;
  /** GitHub logins offered after `@`. */
  people: string[];
  /** Issues offered after `#`. */
  issues: IssueRef[];
  disabled?: boolean;
  placeholder?: string;
  "aria-label": string;
}

/** coss Textarea with `@login` and `#issue` completion. The list is a combobox popup above the field; Enter or Tab accepts. */
export function MentionTextarea({ value, onChange, onSubmit, people, issues, disabled, placeholder, "aria-label": label }: MentionTextareaProps) {
  const id = useId();
  const field = useRef<HTMLTextAreaElement>(null);
  const [caret, setCaret] = useState(0);
  const [active, setActive] = useState(0);
  const [dismissed, setDismissed] = useState<string | null>(null);
  const trigger = useMemo(() => activeTrigger(value, caret), [value, caret]);
  const triggerKey = trigger ? `${trigger.start}:${trigger.sigil}${trigger.query}` : null;
  const options = useMemo(() => (trigger && triggerKey !== dismissed ? suggestMentions(trigger, people, issues) : []), [trigger, triggerKey, dismissed, people, issues]);
  const open = options.length > 0;
  const current = Math.min(active, options.length - 1);

  const accept = (option: MentionOption) => {
    if (!trigger) return;
    const next = applyMention(value, caret, trigger, option);
    onChange(next.text);
    setCaret(next.caret);
    // The textarea's value is written in a layout effect after this commit; place the caret once it has.
    requestAnimationFrame(() => { field.current?.focus(); field.current?.setSelectionRange(next.caret, next.caret); });
  };
  const sync = (event: { currentTarget: HTMLTextAreaElement }) => setCaret(event.currentTarget.selectionStart);

  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.nativeEvent.isComposing) return;
    if (open) {
      if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        event.preventDefault();
        setActive((current + (event.key === "ArrowDown" ? 1 : options.length - 1)) % options.length);
        return;
      }
      if ((event.key === "Enter" && !event.ctrlKey && !event.metaKey && !event.shiftKey) || event.key === "Tab") {
        event.preventDefault();
        accept(options[current]!);
        return;
      }
      if (event.key === "Escape") {
        event.preventDefault(); event.stopPropagation();
        setDismissed(triggerKey);
        return;
      }
    }
    if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) { event.preventDefault(); onSubmit(); }
  };

  return <div className="relative w-full">
    {open && <ul id={`${id}-list`} role="listbox" aria-label={trigger?.sigil === "@" ? "People" : "Issues"} className="absolute bottom-full z-20 mb-1 max-h-56 w-full max-w-sm overflow-auto rounded-lg border bg-popover p-1 text-popover-foreground shadow-lg/5">
      {options.map((option, index) => <li key={option.key} id={`${id}-option-${index}`} role="option" aria-selected={index === current}
        onMouseDown={(event) => { event.preventDefault(); accept(option); }} onMouseMove={() => setActive(index)}
        className={cn("flex cursor-pointer items-baseline gap-2 rounded-md px-2 py-1 text-sm", index === current && "bg-accent text-accent-foreground")}>
        <span className="shrink-0 font-medium">{option.label}</span>
        {option.hint && <span className="truncate text-muted-foreground text-xs">{option.hint}</span>}
      </li>)}
    </ul>}
    <Textarea ref={field} value={value} disabled={disabled} placeholder={placeholder} aria-label={label} role="combobox" aria-autocomplete="list" aria-expanded={open}
      aria-controls={open ? `${id}-list` : undefined} aria-activedescendant={open ? `${id}-option-${current}` : undefined}
      className="[&_textarea]:min-h-24"
      onChange={(event) => { onChange(event.target.value); sync(event); setActive(0); setDismissed(null); }}
      onKeyUp={sync} onClick={sync} onSelect={sync} onKeyDown={onKeyDown} onBlur={() => setDismissed(triggerKey)} />
  </div>;
}
