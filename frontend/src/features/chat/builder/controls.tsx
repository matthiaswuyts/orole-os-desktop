import { useId, useState, type ReactNode } from "react";
import { Plus, X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Combobox, ComboboxChip, ComboboxChips, ComboboxChipsInput, ComboboxEmpty, ComboboxItem, ComboboxList, ComboboxPopup, ComboboxValue } from "@/components/ui/combobox";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import { parseDomain } from "./botForm";

/** A labelled control with optional hint and inline validation message. */
export function Block({ label, hint, error, children, className, aside }: {
  label: string; hint?: ReactNode; error?: string; children: (id: string, describedBy: string | undefined) => ReactNode; className?: string; aside?: ReactNode;
}) {
  const id = useId();
  const describedBy = [hint ? `${id}-hint` : "", error ? `${id}-error` : ""].filter(Boolean).join(" ") || undefined;
  return (
    <div className={cn("flex min-w-0 flex-col gap-1.5", className)}>
      <div className="flex items-baseline justify-between gap-2">
        <label htmlFor={id} className="font-medium text-sm">{label}</label>
        {aside}
      </div>
      {children(id, describedBy)}
      {hint && <p id={`${id}-hint`} className="text-muted-foreground text-xs">{hint}</p>}
      {error && <p id={`${id}-error`} role="alert" className="text-destructive-foreground text-xs">{error}</p>}
    </div>
  );
}

/** A titled group inside a tab. */
export function Section({ title, description, children, id }: { title: string; description?: ReactNode; children: ReactNode; id?: string }) {
  const heading = useId();
  return (
    <section id={id} aria-labelledby={heading} className="flex min-w-0 flex-col gap-3 rounded-lg border bg-card p-3">
      <div className="flex flex-col gap-0.5">
        <h3 id={heading} className="font-medium text-sm">{title}</h3>
        {description && <p className="text-muted-foreground text-xs">{description}</p>}
      </div>
      {children}
    </section>
  );
}

export interface MultiItem {
  value: string;
  label: string;
  /** Second line in the list, e.g. the MCP server or a skill description. */
  detail?: string;
  badge?: string;
}

/** Searchable multi-select over string values. Selected values missing from `items` stay visible and are flagged. */
export function MultiSelect({ id, label, items, value, onChange, placeholder, emptyText, invalid, describedBy, missingLabel = "no longer available" }: {
  id: string; label: string; items: MultiItem[]; value: string[]; onChange: (value: string[]) => void; placeholder: string; emptyText: string;
  invalid?: boolean; describedBy?: string; missingLabel?: string;
}) {
  const known = new Set(items.map((i) => i.value));
  const all: MultiItem[] = [...value.filter((v) => !known.has(v)).map((v) => ({ value: v, label: v, badge: missingLabel })), ...items];
  const selected = value.map((v) => all.find((i) => i.value === v)!);
  return (
    <Combobox<MultiItem, true>
      multiple
      items={all}
      value={selected}
      onValueChange={(next) => onChange(next.map((i) => i.value))}
      itemToStringLabel={(item) => item.label}
      isItemEqualToValue={(a, b) => a.value === b.value}
      filter={(item, query) => `${item.label} ${item.detail ?? ""}`.toLowerCase().includes(query.trim().toLowerCase())}
    >
      <ComboboxChips aria-label={label}>
        <ComboboxValue>
          {(chips: MultiItem[]) => (
            <>
              {chips.map((item) => (
                <ComboboxChip key={item.value} aria-label={item.label} className={item.badge ? "text-warning-foreground" : undefined}>{item.label}</ComboboxChip>
              ))}
              <ComboboxChipsInput id={id} aria-label={`Search ${label}`} aria-invalid={invalid || undefined} aria-describedby={describedBy} placeholder={chips.length ? "" : placeholder} />
            </>
          )}
        </ComboboxValue>
      </ComboboxChips>
      <ComboboxPopup>
        <ComboboxEmpty>{emptyText}</ComboboxEmpty>
        <ComboboxList>
          {(item: MultiItem) => (
            <ComboboxItem key={item.value} value={item}>
              <div className="flex min-w-0 flex-col gap-0.5">
                <span className="flex items-center gap-1.5">
                  <span className="truncate">{item.label}</span>
                  {item.badge && <Badge size="sm" variant="warning">{item.badge}</Badge>}
                </span>
                {item.detail && <span className="line-clamp-2 text-muted-foreground text-xs">{item.detail}</span>}
              </div>
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxPopup>
    </Combobox>
  );
}

/** A list of host names edited as chips. Entries are validated before they are added. */
export function DomainList({ label, value, onChange, max, hint }: { label: string; value: string[]; onChange: (value: string[]) => void; max: number; hint: string }) {
  const id = useId();
  const [draft, setDraft] = useState("");
  const [error, setError] = useState<string | null>(null);
  const add = () => {
    const text = draft.trim();
    if (!text) return;
    const parsed = parseDomain(text);
    if (!parsed.ok) { setError(parsed.error); return; }
    if (value.includes(parsed.domain)) { setError(`${parsed.domain} is already in the list.`); return; }
    if (value.length >= max) { setError(`At most ${max} domains.`); return; }
    onChange([...value, parsed.domain]);
    setDraft("");
    setError(null);
  };
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <label htmlFor={id} className="font-medium text-sm">{label}</label>
      <div className="flex gap-1.5">
        <Input
          nativeInput size="sm" id={id} value={draft} placeholder="example.com" autoComplete="off" spellCheck={false}
          aria-invalid={!!error || undefined} aria-describedby={`${id}-hint`}
          onChange={(e) => { setDraft(e.target.value); setError(null); }}
          onKeyDown={(e) => { if ((e.key === "Enter" || e.key === ",") && !e.nativeEvent.isComposing) { e.preventDefault(); add(); } }}
        />
        <Button type="button" size="sm" variant="outline" onClick={add} disabled={!draft.trim()} aria-label={`Add to ${label}`}><Plus />Add</Button>
      </div>
      {error && <p role="alert" className="text-destructive-foreground text-xs">{error}</p>}
      {value.length > 0 && (
        <ul aria-label={label} className="flex flex-wrap gap-1">
          {value.map((domain) => (
            <li key={domain} className="flex items-center gap-0.5 rounded-md bg-accent ps-2 text-accent-foreground text-xs">
              <span className="selectable">{domain}</span>
              <button type="button" aria-label={`Remove ${domain}`} className="grid size-5 place-items-center rounded opacity-80 hover:opacity-100" onClick={() => onChange(value.filter((d) => d !== domain))}><X className="size-3" /></button>
            </li>
          ))}
        </ul>
      )}
      <p id={`${id}-hint`} className="text-muted-foreground text-xs">{hint} ({value.length}/{max})</p>
    </div>
  );
}
