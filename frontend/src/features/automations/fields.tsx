import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { Plus, X } from "lucide-react";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Switch } from "@/components/ui/switch";
import { Select, SelectGroup, SelectGroupLabel, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { cn } from "@/lib/utils";

export function Failure({ message }: { message: string }) {
  return <Alert variant="error"><AlertTitle>Automation request failed</AlertTitle><AlertDescription>{message}</AlertDescription></Alert>;
}

export function FormField({ label, hint, issue, children }: { label: string; hint?: ReactNode; issue?: string; children: (id: string) => ReactNode }) {
  const id = useId();
  return <Field data-invalid={!!issue || undefined} className="w-full gap-1.5">
    <FieldLabel htmlFor={id}>{label}</FieldLabel>
    {children(id)}
    {hint && <FieldDescription>{hint}</FieldDescription>}
    {issue && <p role="alert" className="text-xs text-destructive-foreground">{issue}</p>}
  </Field>;
}

export function TextField({ label, value, onChange, onBlur, hint, issue, placeholder, multiline = false, mono = false }: { label: string; value: string; onChange: (value: string) => void; onBlur?: () => void; hint?: ReactNode; issue?: string; placeholder?: string; multiline?: boolean; mono?: boolean }) {
  return <FormField label={label} hint={hint} issue={issue}>{(id) => multiline
    ? <Textarea id={id} size="sm" value={value} placeholder={placeholder} aria-invalid={!!issue} className={cn(mono && "font-mono")} onBlur={onBlur} onChange={(e) => onChange(e.target.value)} />
    : <Input id={id} size="sm" value={value} placeholder={placeholder} aria-invalid={!!issue} className={cn(mono && "font-mono")} onBlur={onBlur} onChange={(e) => onChange(e.target.value)} />}</FormField>;
}

export function NumberField({ label, value, onChange, min = 0, max, hint, issue }: { label: string; value: number; onChange: (value: number) => void; min?: number; max?: number; hint?: ReactNode; issue?: string }) {
  return <FormField label={label} hint={hint} issue={issue}>{(id) => <Input id={id} size="sm" type="number" min={min} max={max} step={1} value={value} aria-invalid={!!issue} onChange={(e) => onChange(e.target.value === "" ? 0 : Number(e.target.value))} />}</FormField>;
}

export function SwitchField({ label, checked, onChange, hint, issue }: { label: string; checked: boolean; onChange: (checked: boolean) => void; hint?: ReactNode; issue?: string }) {
  const id = useId();
  return <Field data-invalid={!!issue || undefined} className="w-full gap-1">
    <div className="flex w-full items-center justify-between gap-3"><FieldLabel htmlFor={id}>{label}</FieldLabel><Switch id={id} checked={checked} onCheckedChange={onChange} /></div>
    {hint && <FieldDescription>{hint}</FieldDescription>}
    {issue && <p role="alert" className="text-xs text-destructive-foreground">{issue}</p>}
  </Field>;
}

export interface Choice { value: string; label: string; disabled?: boolean; group?: string }
export function ChoiceField({ label, value, onChange, options, hint, issue, placeholder = "Choose…" }: { label: string; value: string; onChange: (value: string) => void; options: Choice[]; hint?: ReactNode; issue?: string; placeholder?: string }) {
  const groups = [...new Set(options.map((o) => o.group ?? ""))];
  const item = (o: Choice) => <SelectItem key={o.value} value={o.value} disabled={o.disabled}>{o.label}</SelectItem>;
  return <FormField label={label} hint={hint} issue={issue}>{(id) => <Select value={value || null} items={options} onValueChange={(v) => { if (v !== null) onChange(v); }}>
    <SelectTrigger id={id} size="sm" aria-invalid={!!issue}><SelectValue placeholder={placeholder} /></SelectTrigger>
    <SelectPopup alignItemWithTrigger={false}>{groups.map((group) => <SelectGroup key={group}>{group && <SelectGroupLabel>{group}</SelectGroupLabel>}{options.filter((o) => (o.group ?? "") === group).map(item)}</SelectGroup>)}</SelectPopup>
  </Select>}</FormField>;
}

/** A list edited as one entry per line; blank lines are dropped. Remount (key) to reset from outside. */
export function LinesField({ label, value, onChange, hint, issue, mono = false }: { label: string; value: string[]; onChange: (value: string[]) => void; hint?: ReactNode; issue?: string; mono?: boolean }) {
  const [text, setText] = useState(() => value.join("\n"));
  return <TextField label={label} multiline mono={mono} value={text} hint={hint ?? "One per line."} issue={issue} onChange={(next) => { setText(next); onChange(next.split("\n").map((line) => line.trim()).filter(Boolean)); }} />;
}

/** Text key/value rows. Rows with an empty key stay local until they get one. */
export function MapField({ label, value, onChange, hint, issue }: { label: string; value: Record<string, string>; onChange: (value: Record<string, string>) => void; hint?: ReactNode; issue?: string }) {
  const [rows, setRows] = useState(() => Object.entries(value).map(([k, v]) => ({ k, v })));
  const commit = (next: { k: string; v: string }[]) => { setRows(next); onChange(Object.fromEntries(next.filter((r) => r.k.trim()).map((r) => [r.k.trim(), r.v]))); };
  return <fieldset className="flex w-full min-w-0 flex-col gap-1.5">
    <legend className="mb-1.5 text-sm font-medium">{label}</legend>
    {rows.map((row, i) => <div key={i} className="flex gap-1.5">
      <Input size="sm" aria-label={`${label} name ${i + 1}`} placeholder="Name" value={row.k} className="font-mono" onChange={(e) => commit(rows.map((r, j) => j === i ? { ...r, k: e.target.value } : r))} />
      <Input size="sm" aria-label={`${label} value ${i + 1}`} placeholder="Value" value={row.v} onChange={(e) => commit(rows.map((r, j) => j === i ? { ...r, v: e.target.value } : r))} />
      <Button size="icon-sm" variant="ghost" aria-label={`Remove ${label} row ${i + 1}`} onClick={() => commit(rows.filter((_, j) => j !== i))}><X /></Button>
    </div>)}
    <Button size="xs" variant="outline" className="self-start" onClick={() => setRows([...rows, { k: "", v: "" }])}><Plus data-icon="inline-start" />Add row</Button>
    {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    {issue && <p role="alert" className="text-xs text-destructive-foreground">{issue}</p>}
  </fieldset>;
}

/** A JSON object in a textarea; the value only changes while the text parses, `onInvalid` reports otherwise (and clears on unmount). */
export function JsonField({ label, value, onChange, onInvalid, hint, issue }: { label: string; value: unknown; onChange: (value: Record<string, unknown>) => void; onInvalid: (message: string | null) => void; hint?: ReactNode; issue?: string }) {
  const [text, setText] = useState(() => JSON.stringify(value ?? {}, null, 2));
  const [error, setError] = useState<string | null>(null);
  const report = useRef(onInvalid);
  useEffect(() => { report.current = onInvalid; });
  useEffect(() => () => report.current(null), []);
  return <TextField label={label} multiline mono value={text} hint={hint} issue={error ?? issue} onChange={(next) => {
    setText(next);
    try {
      const parsed: unknown = JSON.parse(next || "{}");
      if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("Enter a JSON object, like {\"key\": \"value\"}.");
      setError(null); onInvalid(null); onChange(parsed as Record<string, unknown>);
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      setError(message); onInvalid(message);
    }
  }} />;
}
