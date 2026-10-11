import { useId, type ReactNode } from "react";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Combobox, ComboboxChip, ComboboxChips, ComboboxChipsInput, ComboboxEmpty, ComboboxItem, ComboboxList, ComboboxPopup, ComboboxValue } from "@/components/ui/combobox";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

/** The executive-assistant option as a selectable card that says what the role does. */
export function ExecutiveCard({ checked, onChange }: { checked: boolean; onChange: (checked: boolean) => void }) {
  const id = useId();
  return (
    <Label className="flex items-start gap-2 rounded-lg border p-3 hover:bg-accent/50 has-data-checked:border-primary/48 has-data-checked:bg-accent/50">
      <Checkbox checked={checked} onCheckedChange={(next) => onChange(next === true)} aria-labelledby={`${id}-title`} aria-describedby={`${id}-hint`} />
      <div className="flex flex-col gap-1">
        <p id={`${id}-title`}>Executive assistant</p>
        <p id={`${id}-hint`} className="font-normal text-muted-foreground text-xs">
          Hands work to your other bots and talks with them to get it done, instead of doing everything itself.
        </p>
      </div>
    </Label>
  );
}

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
    <section id={id} aria-labelledby={heading} className="flex min-w-0 flex-col gap-2">
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
