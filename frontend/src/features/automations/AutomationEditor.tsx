import { useEffect, useState } from "react";
import { ArrowDown, ArrowUp, Plus, Save, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { api, errorText, type Definition, type Issue, type Step, type StepType } from "./api";
import { ChoiceField, Failure, FormField, Issues, ListField, NumberField, ParameterField, TextField } from "./fields";

export function AutomationEditor({ initial, types, offline, onSaved, onClose }: { initial: Definition; types: StepType[]; offline: boolean; onSaved: (definition: Definition) => void; onClose: () => void }) {
  const [definition, setDefinition] = useState<Definition>(() => structuredClone(initial));
  const [stepKeys, setStepKeys] = useState(() => initial.steps.map(() => crypto.randomUUID()));
  const [issues, setIssues] = useState<Issue[]>([]);
  const [invalid, setInvalid] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [nextType, setNextType] = useState("");
  const [preview, setPreview] = useState<string[] | null>(null);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [previewBusy, setPreviewBusy] = useState(false);
  const [previewRequest, setPreviewRequest] = useState(0);
  const issue = (path: string) => issues.filter((i) => i.path === path).map((i) => i.message).join("; ") || undefined;
  const patch = (value: Partial<Definition>) => setDefinition((d) => ({ ...d, ...value }));
  const trigger = (value: Partial<Definition["trigger"]>) => setDefinition((d) => ({ ...d, trigger: { ...d.trigger, ...value } }));
  const patchStep = (index: number, value: Partial<Step>) => setDefinition((d) => ({ ...d, steps: d.steps.map((s, i) => i === index ? { ...s, ...value } : s) }));
  useEffect(() => {
    let active = true;
    setPreview(null); setPreviewError(null); setPreviewBusy(false);
    if (definition.trigger.type !== "schedule" || !definition.trigger.schedule.trim() || offline) return;
    const timer = window.setTimeout(() => {
      setPreviewBusy(true);
      api.preview(definition.trigger.schedule).then((dates) => { if (active) setPreview(dates ?? []); }, (e) => { if (active) setPreviewError(errorText(e)); }).finally(() => { if (active) setPreviewBusy(false); });
    }, 400);
    return () => { active = false; window.clearTimeout(timer); };
  }, [definition.trigger.type, definition.trigger.schedule, offline, previewRequest]);
  const save = async () => {
    if (busy || offline || Object.keys(invalid).length) return;
    setBusy(true); setError(null); setIssues([]);
    try {
      const result = await api.save(definition);
      setIssues(result.issues ?? []);
      if (result.saved) onSaved(result.definition);
      else if (!(result.issues ?? []).length) setError("The daemon did not save this definition. Refresh and try again.");
    } catch (e) { setError(errorText(e)); }
    finally { setBusy(false); }
  };
  const move = (index: number, delta: number) => {
    const steps = [...definition.steps];
    [steps[index], steps[index + delta]] = [steps[index + delta]!, steps[index]!];
    const keys = [...stepKeys];
    [keys[index], keys[index + delta]] = [keys[index + delta]!, keys[index]!];
    setStepKeys(keys);
    patch({ steps }); setIssues([]);
  };
  return <form onSubmit={(e) => { e.preventDefault(); if (!Object.keys(invalid).length) void save(); }} className="flex flex-col gap-4">
    <header className="flex flex-wrap items-center justify-between gap-2"><h2 className="text-base font-medium">{initial.id ? "Edit automation" : "New automation"}</h2><Button size="sm" variant="outline" onClick={onClose} disabled={busy}>Close editor</Button></header>
    {offline && <Alert variant="warning"><AlertTitle>Daemon offline</AlertTitle><AlertDescription>Your draft stays here. Reconnect before saving.</AlertDescription></Alert>}
    {error && <Failure message={error} />}<Issues issues={issues} />
    <fieldset disabled={busy} className="flex min-w-0 flex-col gap-4">
      <TextField label="Name" value={definition.name} onChange={(name) => patch({ name })} issue={issue("name")} />
      <TextField label="Description" value={definition.description} multiline onChange={(description) => patch({ description })} issue={issue("description")} />
      <FormField label="Enabled" hint="Disabled automations do not respond to automatic triggers.">{(id) => <Switch id={id} checked={definition.enabled} onCheckedChange={(enabled) => patch({ enabled })} />}</FormField>
      <fieldset className="flex min-w-0 flex-col gap-3"><legend className="mb-3 text-sm font-medium">Trigger</legend>
        <ChoiceField label="Start when" value={definition.trigger.type} options={["manual", "schedule", "event", "webhook"].map((v) => ({ value: v, label: v }))} onChange={(type) => trigger({ type })} issue={issue("trigger.type")} />
        {definition.trigger.type === "schedule" && <>
          <TextField label="Cron schedule" value={definition.trigger.schedule} onChange={(schedule) => trigger({ schedule })} hint="The daemon validates the schedule and calculates the next five runs." issue={issue("trigger.schedule")} />
          <ChoiceField label="Missed schedules" value={definition.trigger.catchUp} options={[{ value: "skip", label: "Skip missed runs" }, { value: "run-once", label: "Catch up once" }]} onChange={(catchUp) => trigger({ catchUp })} issue={issue("trigger.catchUp")} />
          <div className="flex flex-col gap-2" aria-live="polite"><div className="flex items-center gap-2"><h3 className="font-medium text-sm">Next five runs</h3><Button size="xs" variant="outline" disabled={offline || previewBusy || !definition.trigger.schedule.trim()} onClick={() => setPreviewRequest((n) => n + 1)}>Refresh preview</Button></div>{previewBusy && <p role="status">Calculating schedule…</p>}{previewError && <p role="alert" className="text-destructive-foreground">{previewError}</p>}{preview && (preview.length ? <ol className="flex flex-col gap-1 text-xs tabular-nums">{preview.map((date, i) => <li key={`${date}:${i}`}><time dateTime={date}>{new Date(date).toLocaleString()}</time></li>)}</ol> : <p className="text-muted-foreground">No upcoming runs returned.</p>)}</div>
        </>}
        {definition.trigger.type === "event" && <TextField label="Event topic" value={definition.trigger.event} onChange={(event) => trigger({ event })} hint="An exact topic or prefix.*. Event payloads are untrusted data." issue={issue("trigger.event")} />}
        {(definition.trigger.type === "event" || definition.trigger.type === "webhook") && <TextField label="Trigger filter expression" value={definition.trigger.filter} onChange={(filter) => trigger({ filter })} issue={issue("trigger.filter")} />}
        {definition.trigger.type === "webhook" && <p className="text-xs text-muted-foreground">Enable the loopback webhook listener in Settings → Automations. Authentication stays in Windows Credential Manager, never in this editor.</p>}
      </fieldset>
      <section className="flex flex-col gap-3"><h3 className="text-sm font-medium">Steps</h3><p className="text-xs text-muted-foreground">Steps run in order. Stable step IDs are used by branch targets and expressions. Parameters must not contain secrets.</p>
        {!definition.steps.length && <p className="text-muted-foreground">Add a step to define what this automation does.</p>}
        {definition.steps.map((step, index) => {
          const path = `steps.${index}`;
          const spec = types.find((t) => t.type === step.type);
          const stepKey = stepKeys[index]!;
          return <fieldset key={stepKey} className="flex min-w-0 flex-col gap-3 rounded-lg border p-3"><legend className="px-1 font-medium">{index + 1}. {spec?.label ?? step.type}</legend>
            <div className="flex flex-wrap justify-end gap-1"><Button size="xs" variant="outline" disabled={index === 0} onClick={() => move(index, -1)} aria-label={`Move step ${index + 1} up`}><ArrowUp data-icon="inline-start" />Up</Button><Button size="xs" variant="outline" disabled={index === definition.steps.length - 1} onClick={() => move(index, 1)} aria-label={`Move step ${index + 1} down`}><ArrowDown data-icon="inline-start" />Down</Button><Button size="xs" variant="destructive-outline" onClick={() => { patch({ steps: definition.steps.filter((_, i) => i !== index) }); setStepKeys(stepKeys.filter((_, i) => i !== index)); setIssues([]); setInvalid((previous) => Object.fromEntries(Object.entries(previous).filter(([key]) => !key.startsWith(`${stepKey}.`)))); }}><Trash2 data-icon="inline-start" />Remove</Button></div>
            {!spec?.available && <Alert variant="warning"><AlertTitle>Step unavailable</AlertTitle><AlertDescription>{spec?.reason || "This step type is not supported by the connected daemon."}</AlertDescription></Alert>}
            <div className="grid gap-3 sm:grid-cols-2"><TextField label="Step ID" value={step.id} issue={issue(`${path}.id`)} onChange={(id) => patchStep(index, { id })} /><TextField label="Step name" value={step.name} onChange={(name) => patchStep(index, { name })} /></div>
            {(spec?.fields ?? []).map((field) => <ParameterField key={field.key} field={field} value={step.params?.[field.key]} issue={issue(`${path}.params.${field.key}`)} onChange={(value) => patchStep(index, { params: { ...step.params, [field.key]: value } })} invalid={(message) => setInvalid((previous) => { const next = { ...previous }; const key = `${stepKey}.${field.key}`; if (message) next[key] = message; else delete next[key]; return next; })} />)}
            <details><summary className="cursor-pointer text-sm font-medium">Conditions and execution policy</summary><div className="mt-3 flex flex-col gap-3">
              <TextField label="Run only when" value={step.when} onChange={(when) => patchStep(index, { when })} hint="Optional expression evaluated before this step." issue={issue(`${path}.when`)} />
              <div className="grid gap-3 sm:grid-cols-3"><NumberField label="Timeout (seconds)" value={step.timeoutSec} min={1} max={86400} onChange={(timeoutSec) => patchStep(index, { timeoutSec })} issue={issue(`${path}.timeoutSec`)} /><NumberField label="Retries" value={step.retries} max={8} onChange={(retries) => patchStep(index, { retries })} issue={issue(`${path}.retries`)} /><NumberField label="Backoff (milliseconds)" value={step.backoffMs} max={60000} onChange={(backoffMs) => patchStep(index, { backoffMs })} issue={issue(`${path}.backoffMs`)} /></div>
              <ChoiceField label="On failure" value={step.onError} options={[{ value: "stop", label: "Stop the run" }, { value: "continue", label: "Continue to next step" }]} onChange={(onError) => patchStep(index, { onError })} issue={issue(`${path}.onError`)} />
              <ChoiceField label="Repeat policy" value={step.strategy} options={[{ value: "at-most-once", label: "At most once" }, { value: "key", label: "Deduplicate with an idempotency key", disabled: !spec?.keySupported }, { value: "repeatable", label: "I confirm this action is safe to repeat" }]} onChange={(strategy) => patchStep(index, { strategy })} issue={issue(`${path}.strategy`)} hint="Repeating a side effect can duplicate changes. The daemon enforces the policy." />
            </div></details>
          </fieldset>;
        })}
        <div className="flex flex-wrap items-end gap-2"><div className="min-w-0 flex-1"><ChoiceField label="Step type" value={nextType} onChange={setNextType} options={types.map((t) => ({ value: t.type, label: t.available ? t.label : `${t.label} — ${t.reason || "Unavailable"}`, disabled: !t.available }))} /></div><Button size="sm" variant="outline" disabled={!types.some((t) => t.type === nextType && t.available)} onClick={() => {
          let n = definition.steps.length + 1;
          while (definition.steps.some((s) => s.id === `s${n}`)) n++;
          const spec = types.find((t) => t.type === nextType);
          patch({ steps: [...definition.steps, { id: `s${n}`, name: "", type: nextType, params: Object.fromEntries((spec?.fields ?? []).filter((f) => f.kind === "enum" && f.options?.length).map((f) => [f.key, f.options[0]])), when: "", timeoutSec: 300, retries: 0, backoffMs: 1000, onError: "stop", strategy: "at-most-once" }] });
          setStepKeys([...stepKeys, crypto.randomUUID()]);
        }}><Plus data-icon="inline-start" />Add step</Button></div>
      </section>
      <details><summary className="cursor-pointer text-sm font-medium">Limits and permissions</summary><div className="mt-3 flex flex-col gap-3"><div className="grid gap-3 sm:grid-cols-2"><NumberField label="Concurrent runs" value={definition.concurrency} max={32} hint="0 uses the default in Settings." onChange={(concurrency) => patch({ concurrency })} issue={issue("concurrency")} /><NumberField label="Run timeout (seconds)" value={definition.timeoutSec} min={1} max={86400} onChange={(timeoutSec) => patch({ timeoutSec })} issue={issue("timeoutSec")} /></div><ListField label="Allowed executables" value={definition.allowedExecutables ?? []} onChange={(allowedExecutables) => patch({ allowedExecutables })} hint="Exact executable names or paths used by command and test steps." issue={issue("allowedExecutables")} /><ListField label="Allowed HTTP hosts" value={definition.allowedHosts ?? []} onChange={(allowedHosts) => patch({ allowedHosts })} issue={issue("allowedHosts")} /><FormField label="Allow HTTP steps" hint="Outbound requests are still checked by the daemon host policy.">{(id) => <Switch id={id} checked={definition.allowHttp} onCheckedChange={(allowHttp) => patch({ allowHttp })} />}</FormField></div></details>
    </fieldset>
    {Object.keys(invalid).length > 0 && <p role="alert" className="text-destructive-foreground">Fix the invalid parameter fields before saving.</p>}
    <footer className="flex items-center gap-2"><Button type="submit" size="sm" disabled={busy || offline || Object.keys(invalid).length > 0}><Save data-icon="inline-start" />{busy ? "Saving…" : "Save automation"}</Button><Button size="sm" variant="outline" disabled={busy} onClick={onClose}>Discard draft</Button></footer>
  </form>;
}
