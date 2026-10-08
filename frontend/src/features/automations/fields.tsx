import { useId, useState, type ReactNode } from "react";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Select, SelectGroup, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import type { Issue, SchemaField } from "./api";

export function Failure({ message }: { message: string }) {
  return <Alert variant="error"><AlertTitle>Automation request failed</AlertTitle><AlertDescription>{message}</AlertDescription></Alert>;
}
export function FormField({ label, hint, issue, children }: { label: string; hint?: string; issue?: string; children: (id: string) => ReactNode }) {
  const id = useId();
  return <Field data-invalid={!!issue || undefined} className="w-full"><FieldLabel htmlFor={id}>{label}</FieldLabel>{children(id)}{hint && <FieldDescription>{hint}</FieldDescription>}{issue && <p role="alert" className="text-xs text-destructive-foreground">{issue}</p>}</Field>;
}
export function TextField({ label, value, onChange, hint, issue, multiline = false }: { label: string; value: string; onChange: (value: string) => void; hint?: string; issue?: string; multiline?: boolean }) {
  return <FormField label={label} hint={hint} issue={issue}>{(id) => multiline ? <Textarea id={id} value={value} aria-invalid={!!issue} onChange={(e) => onChange(e.target.value)} /> : <Input id={id} value={value} aria-invalid={!!issue} onChange={(e) => onChange(e.target.value)} />}</FormField>;
}
export function NumberField({ label, value, onChange, min = 0, max, hint, issue }: { label: string; value: number; onChange: (value: number) => void; min?: number; max?: number; hint?: string; issue?: string }) {
  return <FormField label={label} hint={hint} issue={issue}>{(id) => <Input id={id} type="number" min={min} max={max} step={1} value={value} aria-invalid={!!issue} onChange={(e) => onChange(e.target.value === "" ? 0 : Number(e.target.value))} />}</FormField>;
}
export interface Choice { value: string; label: string; disabled?: boolean }
export function ChoiceField({ label, value, onChange, options, hint, issue }: { label: string; value: string; onChange: (value: string) => void; options: Choice[]; hint?: string; issue?: string }) {
  return <FormField label={label} hint={hint} issue={issue}>{(id) => <Select value={value} items={options} onValueChange={(v) => { if (v !== null) onChange(v); }}><SelectTrigger id={id} size="sm" aria-invalid={!!issue}><SelectValue placeholder="Choose…" /></SelectTrigger><SelectPopup><SelectGroup>{options.map((o) => <SelectItem key={o.value} value={o.value} disabled={o.disabled}>{o.label}</SelectItem>)}</SelectGroup></SelectPopup></Select>}</FormField>;
}
export function ListField({ label, value, onChange, hint, issue }: { label: string; value: string[]; onChange: (value: string[]) => void; hint?: string; issue?: string }) {
  return <fieldset className="flex min-w-0 flex-col gap-2"><legend className="mb-2 text-sm font-medium">{label}</legend>{hint && <p className="text-xs text-muted-foreground">{hint}</p>}{value.map((item, index) => <div key={index} className="flex gap-2"><Input aria-label={`${label} ${index + 1}`} value={item} aria-invalid={!!issue} onChange={(e) => onChange(value.map((v, i) => i === index ? e.target.value : v))} /><Button size="sm" variant="outline" aria-label={`Remove ${label} ${index + 1}`} onClick={() => onChange(value.filter((_, i) => i !== index))}>Remove</Button></div>)}<Button className="self-start" size="xs" variant="outline" onClick={() => onChange([...value, ""])}>Add item</Button>{issue && <p role="alert" className="text-xs text-destructive-foreground">{issue}</p>}</fieldset>;
}
function ObjectField({ field, value, onChange, issue, invalid }: { field: SchemaField; value: unknown; onChange: (value: unknown) => void; issue?: string; invalid: (message: string | null) => void }) {
  const [text, setText] = useState(() => JSON.stringify(value ?? {}, null, 2));
  const [error, setError] = useState<string | null>(null);
  return <TextField label={field.label} multiline value={text} hint={field.hint || (field.kind === "map" ? "JSON object with text values. Never enter credentials here." : "JSON object. Never enter credentials here.")} issue={error ?? issue} onChange={(next) => {
    setText(next);
    try {
      const parsed: unknown = JSON.parse(next);
      if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("Enter a JSON object.");
      if (field.kind === "map" && Object.values(parsed).some((v) => typeof v !== "string")) throw new Error("Every value must be text.");
      setError(null); invalid(null); onChange(parsed);
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      setError(message); invalid(message);
    }
  }} />;
}
export function ParameterField({ field, value, onChange, issue, invalid }: { field: SchemaField; value: unknown; onChange: (value: unknown) => void; issue?: string; invalid: (message: string | null) => void }) {
  const label = field.required ? `${field.label} (required)` : field.label;
  switch (field.kind) {
    case "map": case "json": return <ObjectField field={{ ...field, label }} value={value} onChange={onChange} issue={issue} invalid={invalid} />;
    case "list": return <ListField label={label} value={Array.isArray(value) ? value.map(String) : []} onChange={onChange} hint={field.hint} issue={issue} />;
    case "number": return <NumberField label={label} value={typeof value === "number" ? value : 0} onChange={onChange} max={86400} hint={field.hint} issue={issue} />;
    case "enum": return <ChoiceField label={label} value={typeof value === "string" ? value : ""} onChange={onChange} options={(field.options ?? []).map((v) => ({ value: v, label: v }))} hint={field.hint} issue={issue} />;
    default: return <TextField label={label} value={typeof value === "string" ? value : ""} onChange={onChange} multiline={field.kind === "text"} hint={field.hint || (field.kind === "expr" ? "Evaluated by the daemon expression engine." : undefined)} issue={issue} />;
  }
}
export function Issues({ issues }: { issues: Issue[] }) {
  if (!issues.length) return null;
  return <Alert variant="error"><AlertTitle>Changes were not saved</AlertTitle><AlertDescription><ul className="flex flex-col gap-1">{issues.map((issue, i) => <li key={`${issue.path}:${i}`}><code>{issue.path}</code>: {issue.message}</li>)}</ul></AlertDescription></Alert>;
}
