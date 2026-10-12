import { useState } from "react";
import { Copy, Trash2, X } from "lucide-react";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { useHarnessOptions } from "@/features/agents/useHarnessOptions";
import type { SchemaField, Step, StepType, Topic, Trigger } from "./api";
import { ChoiceField, JsonField, LinesField, MapField, NumberField, SwitchField, TextField, type Choice } from "./fields";
import { earlierSteps, eventFields, params } from "./flow";
import { BlockIcon } from "./BlockIcon";
import { useNames } from "./pickers";

type Issues = Record<string, string[]>;
const ADVANCED = ["id", "name", "when", "timeoutSec", "retries", "backoffMs", "onError", "strategy"];

/** Keeps a stored value that is not among the choices (a template, or a deleted bot) visible instead of blank. */
function withCurrent(options: Choice[], value: string): Choice[] {
  return value && !options.some((o) => o.value === value) ? [...options, { value, label: value.includes("{{") ? value : `${value} (not found)` }] : options;
}

function HarnessField({ label, value, onChange, issue, hint }: { label: string; value: string; onChange: (value: string) => void; issue?: string; hint?: string }) {
  const { options, error, loading } = useHarnessOptions("local");
  const choices = (options?.harnesses ?? []).map((h) => ({ value: h.id, label: h.available ? h.name : `${h.name} (${h.unavailableReason || "not installed"})`, disabled: !h.available }));
  return <ChoiceField label={label} value={value} onChange={onChange} issue={issue} options={withCurrent(choices, value)} placeholder={loading ? "Detecting agents…" : "Choose an agent…"}
    hint={error ? `Could not detect agents: ${error}` : hint} />;
}

function ParamField({ field, value, onChange, issue, onInvalid }: { field: SchemaField; value: unknown; onChange: (value: unknown) => void; issue?: string; onInvalid: (message: string | null) => void }) {
  const { bots, projects } = useNames();
  const label = field.required || field.kind === "boolean" ? field.label : `${field.label} (optional)`;
  const text = typeof value === "string" ? value : "";
  const hint = field.hint || undefined;
  switch (field.kind) {
    case "text": return <TextField label={label} multiline value={text} onChange={onChange} issue={issue} hint={hint} />;
    case "expr": return <TextField label={label} mono value={text} onChange={onChange} issue={issue} hint={hint ?? "An expression the daemon evaluates, like steps.check.output.failed > 0."} />;
    case "number": return <NumberField label={label} value={typeof value === "number" ? value : 0} max={86400} onChange={onChange} issue={issue} hint={hint} />;
    case "boolean": return <SwitchField label={label} checked={value === true} onChange={onChange} issue={issue} hint={hint} />;
    case "enum": return <ChoiceField label={label} value={text} onChange={onChange} issue={issue} hint={hint} options={withCurrent((field.options ?? []).map((o) => ({ value: o, label: o })), text)} />;
    case "list": return <LinesField label={label} value={Array.isArray(value) ? value.map(String) : []} onChange={onChange} issue={issue} hint={hint} />;
    case "map": return <MapField label={label} value={value && typeof value === "object" && !Array.isArray(value) ? Object.fromEntries(Object.entries(value).map(([k, v]) => [k, String(v)])) : {}} onChange={onChange} issue={issue} hint={hint ?? "Never put credentials here."} />;
    case "json": return <JsonField label={label} value={value} onChange={onChange} onInvalid={onInvalid} issue={issue} hint={hint ?? "A JSON object. Never put credentials here."} />;
    case "bot": return <ChoiceField label={label} value={text} onChange={onChange} issue={issue} placeholder="Choose a bot…" options={withCurrent(bots.map((b) => ({ value: b.id, label: b.name })), text)} hint={bots.length ? hint : "Create a bot in Bots first."} />;
    case "project": return <ChoiceField label={label} value={text} onChange={onChange} issue={issue} placeholder="Choose a project…" options={withCurrent(projects.map((p) => ({ value: p.id, label: p.name })), text)} hint={projects.length ? hint : "Add a project first."} />;
    case "harness": return <HarnessField label={label} value={text} onChange={onChange} issue={issue} hint={hint} />;
    default: return <TextField label={label} value={text} onChange={onChange} issue={issue} hint={hint} />;
  }
}

function Variables({ step, tree, trigger, topics }: { step: Step; tree: Step[]; trigger: Trigger; topics: Topic[] }) {
  const [copied, setCopied] = useState<string | null>(null);
  const vars = [
    ...eventFields(trigger, topics).map((f) => ({ text: `{{ event.data.${f.name} }}`, title: f.label })),
    ...earlierSteps(tree, step.id).map((s) => ({ text: `{{ steps.${s.id}.output }}`, title: s.name || s.type })),
  ];
  if (!vars.length) return null;
  const copy = (text: string) => { void navigator.clipboard?.writeText(text).then(() => setCopied(text), () => setCopied(null)); };
  return <details className="text-xs">
    <summary className="cursor-pointer rounded-sm font-medium outline-none focus-visible:ring-2 focus-visible:ring-ring">Values from earlier steps</summary>
    <p className="mt-1.5 text-muted-foreground">Click to copy, then paste into a field. Event and output text is data, never instructions.</p>
    <ul className="mt-1.5 flex flex-wrap gap-1">{vars.map((v) => <li key={v.text}>
      <button type="button" title={v.title} onClick={() => copy(v.text)} className="inline-flex items-center gap-1 rounded-sm border bg-muted/40 px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground outline-none hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring">
        <Copy aria-hidden className="size-3" />{v.text}
      </button>
    </li>)}</ul>
    <p aria-live="polite" className="mt-1 text-muted-foreground">{copied ? `Copied ${copied}` : ""}</p>
  </details>;
}

export function Inspector({ step, spec, tree, issues, trigger, topics, onChange, onRename, onRemove, onClose, onInvalid }: {
  step: Step; spec: StepType | undefined; tree: Step[]; issues: Issues; trigger: Trigger; topics: Topic[];
  onChange: (step: Step) => void; onRename: (id: string) => string | null; onRemove: () => void; onClose: () => void; onInvalid: (key: string, message: string | null) => void;
}) {
  const [id, setId] = useState(step.id);
  const [idError, setIdError] = useState<string | null>(null);
  const p = params(step);
  const issue = (key: string) => issues[key]?.join("; ");
  const fields = spec?.fields ?? [];
  const shown = new Set([...fields.map((f) => `params.${f.key}`), ...ADVANCED]);
  const other = Object.entries(issues).filter(([key]) => !shown.has(key)).flatMap(([key, list]) => list.map((m) => key ? `${key}: ${m}` : m));
  const condition = step.type === "condition";
  const patch = (value: Partial<Step>) => onChange({ ...step, ...value });
  return <section aria-label={`Block ${step.id}`} className="flex flex-col gap-3 p-3">
    <header className="flex items-start gap-2">
      <span className="flex size-7 shrink-0 items-center justify-center rounded-md border bg-muted/50"><BlockIcon type={step.type} className="size-4" /></span>
      <div className="min-w-0 flex-1">
        <h2 className="truncate text-sm font-medium">{spec?.label ?? step.type}</h2>
        <p className="text-xs text-muted-foreground">{spec?.description ?? "The connected daemon does not know this block type."}</p>
      </div>
      <Button size="icon-xs" variant="ghost" aria-label="Close block settings" onClick={onClose}><X /></Button>
    </header>
    {spec && !spec.available && <Alert variant="warning"><AlertDescription>{spec.reason || "This block is not available on this PC."}</AlertDescription></Alert>}
    {other.length > 0 && <Alert variant="error"><AlertDescription><ul className="flex flex-col gap-0.5">{other.map((m, i) => <li key={i}>{m}</li>)}</ul></AlertDescription></Alert>}
    {fields.map((field) => <ParamField key={field.key} field={field} value={p[field.key]} issue={issue(`params.${field.key}`)}
      onChange={(value) => patch({ params: { ...p, [field.key]: value } })} onInvalid={(message) => onInvalid(`${step.id}.${field.key}`, message)} />)}
    <Variables step={step} tree={tree} trigger={trigger} topics={topics} />
    <details className="flex flex-col" open={ADVANCED.some((key) => issues[key]?.length) || undefined}>
      <summary className="cursor-pointer rounded-sm text-sm font-medium outline-none focus-visible:ring-2 focus-visible:ring-ring">Advanced</summary>
      <div className="mt-3 flex flex-col gap-3">
        <TextField label="Block ID" mono value={id} issue={idError ?? issue("id")} hint="Later blocks read this block's result as steps.<ID>.output."
          onChange={(next) => { setId(next); setIdError(null); }} onBlur={() => { if (id !== step.id) { const problem = onRename(id); setIdError(problem); if (problem) setId(step.id); } }} />
        <TextField label="Name (optional)" value={step.name} issue={issue("name")} placeholder={spec?.label} onChange={(name) => patch({ name })} />
        {!condition && <TextField label="Run only when (optional)" mono value={step.when} issue={issue("when")} placeholder="event.data.status == 'failed'" onChange={(when) => patch({ when })} hint="The block is skipped when this is false." />}
        <div className="grid grid-cols-2 gap-2">
          <NumberField label="Timeout (s)" value={step.timeoutSec} min={1} max={86400} issue={issue("timeoutSec")} onChange={(timeoutSec) => patch({ timeoutSec })} />
          <NumberField label="Retries" value={step.retries} max={8} issue={issue("retries")} onChange={(retries) => patch({ retries })} />
        </div>
        {step.retries > 0 && <NumberField label="Wait between retries (ms)" value={step.backoffMs} max={60000} issue={issue("backoffMs")} onChange={(backoffMs) => patch({ backoffMs })} />}
        {!condition && <ChoiceField label="If it fails" value={step.onError} issue={issue("onError")} onChange={(onError) => patch({ onError })}
          options={[{ value: "stop", label: "Stop the flow" }, { value: "continue", label: "Continue with the next block" }]} />}
        <ChoiceField label="Repeat safety" value={step.strategy} issue={issue("strategy")} onChange={(strategy) => patch({ strategy })} hint="Retrying an action can repeat it. The daemon enforces this."
          options={[{ value: "at-most-once", label: "Run at most once" }, { value: "key", label: "Deduplicate with an idempotency key", disabled: !spec?.keySupported }, { value: "repeatable", label: "Safe to repeat" }]} />
      </div>
    </details>
    <Button size="sm" variant="destructive-outline" className="self-start" onClick={onRemove}><Trash2 data-icon="inline-start" />Remove block</Button>
  </section>;
}
