import { useCallback, useRef, useState } from "react";
import {
  DndContext, DragOverlay, KeyboardSensor, PointerSensor, closestCenter, pointerWithin, useSensor, useSensors,
  type Active, type Announcements, type CollisionDetection, type DragEndEvent, type DragStartEvent, type Over,
} from "@dnd-kit/core";
import { sortableKeyboardCoordinates } from "@dnd-kit/sortable";
import { ArrowLeft, Play, Save } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { AlertDialog, AlertDialogClose, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogPopup, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { api, errorText, type Definition, type Issue, type Step, type StepType, type Topic } from "./api";
import { BlockLibrary } from "./BlockLibrary";
import { BlockIcon } from "./BlockIcon";
import { BlockStack } from "./BlockStack";
import { FlowContext, type DragData, type FlowContextValue } from "./flowContext";
import { Failure, LinesField, NumberField, SwitchField, TextField } from "./fields";
import {
  LANE_LABEL, ROOT, flatten, insertProblem, insertStep, laneSteps, locate, mapIssues, moveStep, newStep, removeStep, updateStep,
  type IssueMap, type Lane,
} from "./flow";
import { Inspector } from "./Inspector";
import { useNames } from "./pickers";
import { RunsPanel } from "./RunsPanel";
import { TriggerCard } from "./TriggerCard";

// Space picks a block up and drops it; Enter stays a click (library items add on Enter).
const KEYS = { start: ["Space"], cancel: ["Escape"], end: ["Space", "Enter"] };
const FLOW_FIELDS: Record<string, string> = {
  name: "Name", description: "Description", steps: "Blocks", timeoutSec: "Run timeout", concurrency: "Concurrent runs",
  allowedExecutables: "Allowed programs", allowedHosts: "Allowed hosts", allowHttp: "Plain HTTP",
};
const SETTINGS = ["description", "timeoutSec", "concurrency", "allowedExecutables", "allowedHosts", "allowHttp"];
type Outcome = "listed" | "saved" | "rejected";

export function FlowBuilder({ initial, initialIssues, initialRun = null, types, topics, offline, onBack, onSaved }: {
  initial: Definition; initialIssues: Issue[]; initialRun?: string | null; types: StepType[]; topics: Topic[]; offline: boolean;
  onBack: () => void; onSaved: (definition: Definition) => void;
}) {
  const [definition, setDefinition] = useState(initial);
  const [baseline, setBaseline] = useState(() => initial.id ? JSON.stringify(initial) : null);
  const [issues, setIssues] = useState<IssueMap>(() => mapIssues(initial.steps ?? [], initialIssues));
  const [outcome, setOutcome] = useState<Outcome>("listed");
  const [selected, setSelected] = useState<string | null>(null);
  const [tab, setTab] = useState<"flow" | "runs">(initialRun && initial.id ? "runs" : "flow");
  const [runFocus, setRunFocus] = useState<string | null>(initialRun);
  const [busy, setBusy] = useState<"save" | "run" | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [invalid, setInvalid] = useState<Record<string, string>>({});
  const [leaving, setLeaving] = useState(false);
  const [dragging, setDragging] = useState<{ id: string; data: DragData } | null>(null);
  const { names } = useNames();
  const steps = definition.steps ?? [];
  const tree = useRef(steps);
  tree.current = steps;
  const dirty = baseline === null || JSON.stringify(definition) !== baseline;
  const issueCount = Object.values(issues.flow).flat().length + Object.values(issues.steps).flatMap((f) => Object.values(f).flat()).length;
  const flowIssue = (path: string) => issues.flow[path]?.join("; ");

  const patch = (value: Partial<Definition>) => setDefinition((d) => ({ ...d, ...value }));
  const setSteps = (next: Step[]) => setDefinition((d) => ({ ...d, steps: next }));
  const markInvalid = useCallback((key: string, message: string | null) => setInvalid((prev) => {
    if (message ? prev[key] === message : !(key in prev)) return prev;
    const next = { ...prev };
    if (message) next[key] = message; else delete next[key];
    return next;
  }), []);

  const insert = (lane: string, index: number, type: string) => {
    const spec = types.find((t) => t.type === type);
    if (!spec?.available) return;
    const block = newStep(spec, steps);
    const next = insertStep(steps, lane, index, block);
    if (!next) { setNotice(insertProblem(steps, lane, block)); return; }
    setNotice(null); setSteps(next); setSelected(block.id);
  };
  const remove = (id: string) => {
    const at = locate(steps, id);
    if (!at) return;
    const gone = new Set(flatten([at.step]).map((x) => x.step.id));
    setSteps(removeStep(steps, id));
    if (selected && gone.has(selected)) setSelected(null);
    setInvalid((prev) => Object.fromEntries(Object.entries(prev).filter(([key]) => !gone.has(key.split(".")[0]!))));
  };
  const rename = (from: string, to: string): string | null => {
    const id = to.trim();
    if (!id) return "Enter an ID.";
    if (!/^[A-Za-z0-9_-]+$/.test(id)) return "Use letters, digits, _ or -.";
    if (id !== from && flatten(steps).some((x) => x.step.id === id)) return "Another block uses this ID.";
    setSteps(updateStep(steps, from, (s) => ({ ...s, id })));
    setIssues((map) => { const { [from]: moved, ...rest } = map.steps; return moved ? { ...map, steps: { ...rest, [id]: moved } } : map; });
    setInvalid((prev) => Object.fromEntries(Object.entries(prev).map(([key, m]) => [key.startsWith(`${from}.`) ? `${id}${key.slice(from.length)}` : key, m])));
    setSelected(id);
    return null;
  };

  const save = async () => {
    if (busy || offline || Object.keys(invalid).length) return;
    setBusy("save"); setError(null); setNotice(null);
    const sent = definition;
    try {
      const result = await api.save(sent);
      const list = result.issues ?? [];
      if (result.saved) {
        setDefinition(result.definition);
        setBaseline(JSON.stringify(result.definition));
        setIssues(mapIssues(result.definition.steps, list));
        setOutcome("saved");
        if (!list.length) setNotice("Saved.");
        onSaved(result.definition);
      } else {
        setIssues(mapIssues(sent.steps, list));
        setOutcome("rejected");
        if (!list.length) setError("The daemon did not save this flow. Try again.");
      }
    } catch (e) { setError(errorText(e)); }
    finally { setBusy(null); }
  };
  const run = async () => {
    if (busy || !definition.id) return;
    setBusy("run"); setError(null); setNotice(null);
    try { const started = await api.run(definition.id); setRunFocus(started.id); setTab("runs"); }
    catch (e) { setError(errorText(e)); }
    finally { setBusy(null); }
  };

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates, keyboardCodes: KEYS }),
  );
  // Nested lanes overlap their condition card: the smallest target under the pointer is the precise one.
  // A block never targets itself from inside (its own lanes or nested blocks).
  const collision = useCallback<CollisionDetection>((args) => {
    const data = args.active.data.current as DragData | undefined;
    let containers = args.droppableContainers;
    if (data?.kind === "block") {
      const moving = locate(tree.current, String(args.active.id));
      const inside = new Set(moving ? flatten([moving.step]).map((x) => x.step.id) : []);
      containers = containers.filter((c) => {
        const d = c.data.current as DragData | undefined;
        return d?.kind === "lane" ? !inside.has(d.lane.split("/")[0]!) : c.id === args.active.id || !inside.has(String(c.id));
      });
    }
    const scoped = { ...args, droppableContainers: containers };
    if (!args.pointerCoordinates) return closestCenter(scoped);
    const hits = pointerWithin(scoped);
    if (!hits.length) return data?.kind === "block" ? closestCenter(scoped) : [];
    const area = (id: string | number) => { const r = args.droppableRects.get(id); return r ? r.width * r.height : Number.POSITIVE_INFINITY; };
    return [...hits].sort((a, b) => area(a.id) - area(b.id));
  }, []);
  const laneName = (lane: string) => lane === ROOT ? "the flow" : `the ${LANE_LABEL[lane.split("/")[1] as Lane]} lane of ${lane.split("/")[0]}`;
  const describe = (item: Active | Over) => {
    const data = item.data.current as DragData | undefined;
    if (data?.kind === "new") return `new ${types.find((t) => t.type === data.type)?.label ?? data.type} block`;
    if (data?.kind === "lane") return `the end of ${laneName(data.lane)}`;
    const at = locate(tree.current, String(item.id));
    return at ? `${at.step.name || at.step.id} in ${laneName(at.lane)}` : String(item.id);
  };
  const announcements: Announcements = {
    onDragStart: ({ active }) => `Picked up ${describe(active)}. Use the arrow keys to move, Space to drop, Escape to cancel.`,
    onDragOver: ({ active, over }) => over ? `${describe(active)} is over ${describe(over)}.` : `${describe(active)} is not over a drop target.`,
    onDragEnd: ({ active, over }) => over ? `Dropped ${describe(active)} at ${describe(over)}.` : `Dropped ${describe(active)}; nothing moved.`,
    onDragCancel: ({ active }) => `Cancelled moving ${describe(active)}.`,
  };
  const onDragStart = ({ active }: DragStartEvent) => { const data = active.data.current as DragData | undefined; setDragging(data ? { id: String(active.id), data } : null); };
  const onDragEnd = ({ active, over }: DragEndEvent) => {
    setDragging(null);
    const from = active.data.current as DragData | undefined;
    const to = over?.data.current as DragData | undefined;
    if (!over || !from || !to || over.id === active.id) return;
    let lane: string;
    let index: number;
    if (to.kind === "lane") { lane = to.lane; index = laneSteps(steps, lane)?.length ?? 0; }
    else { const at = locate(steps, String(over.id)); if (!at) return; lane = at.lane; index = at.index; }
    if (from.kind === "new") { insert(lane, index, from.type); return; }
    const next = moveStep(steps, String(active.id), lane, index);
    if (next) setSteps(next);
    else setNotice("A block cannot move into its own lanes or nest conditions more than 3 deep.");
  };

  const selectedAt = selected ? locate(steps, selected) : null;
  const flow: FlowContextValue = { tree: steps, types, issues, names, selected, select: setSelected, insert, remove };
  const summary = [
    ...Object.entries(issues.flow).flatMap(([path, list]) => list.map((m) => ({ key: `f:${path}:${m}`, text: `${FLOW_FIELDS[path] ?? (path.startsWith("trigger") ? "Trigger" : path)}: ${m}`, id: null as string | null }))),
    ...Object.entries(issues.steps).flatMap(([id, fields]) => Object.entries(fields).flatMap(([field, list]) => list.map((m) => ({ key: `s:${id}:${field}:${m}`, text: `${id}${field ? ` (${field.replace(/^params\./, "")})` : ""}: ${m}`, id })))),
  ];
  const leave = () => { if (dirty) setLeaving(true); else onBack(); };
  const blocked = Object.keys(invalid).length > 0;
  const runBlocker = !definition.id ? "Save the flow first" : dirty ? "Save your changes first; Run now runs the saved flow" : null;
  const runButton = <Button size="sm" variant="outline" disabled={!definition.id || dirty || offline || !!busy} onClick={() => void run()}><Play data-icon="inline-start" />{busy === "run" ? "Starting…" : "Run now"}</Button>;

  return <div className="flex h-full min-h-0 flex-col">
    <header className="flex shrink-0 flex-wrap items-center gap-2 border-b px-3 py-2">
      <Button size="icon-sm" variant="ghost" aria-label="Back to automations" onClick={leave}><ArrowLeft /></Button>
      <Input size="sm" aria-label="Flow name" aria-invalid={!!flowIssue("name")} value={definition.name} onChange={(e) => patch({ name: e.target.value })} className="w-56 min-w-0 font-medium" />
      <div role="tablist" aria-label="Builder view" className="flex rounded-md border p-0.5">
        {(["flow", "runs"] as const).map((t) => { const tabButton = <button key={t} type="button" role="tab" aria-selected={tab === t} disabled={t === "runs" && !definition.id}
          onClick={() => setTab(t)} className={cn("h-6 rounded-sm px-2 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50", tab === t ? "bg-accent font-medium text-foreground" : "text-muted-foreground hover:text-foreground")}>{t === "flow" ? "Flow" : "Runs"}</button>;
          return t === "runs" && !definition.id ? <Tooltip key={t}><TooltipTrigger render={<span className="inline-flex" />}>{tabButton}</TooltipTrigger><TooltipPopup>Save the flow to see its runs</TooltipPopup></Tooltip> : tabButton; })}
      </div>
      <div className="ml-auto flex items-center gap-2">
        <label className="flex items-center gap-2 text-xs text-muted-foreground"><Switch checked={definition.enabled} onCheckedChange={(enabled) => patch({ enabled })} />Enabled</label>
        {runBlocker ? <Tooltip><TooltipTrigger render={<span className="inline-flex" />}>{runButton}</TooltipTrigger><TooltipPopup className="max-w-xs">{runBlocker}</TooltipPopup></Tooltip> : runButton}
        <Button size="sm" disabled={offline || !!busy || blocked} onClick={() => void save()}><Save data-icon="inline-start" />{busy === "save" ? "Saving…" : dirty ? "Save" : "Saved"}</Button>
      </div>
    </header>
    {(offline || error || notice || summary.length > 0 || blocked) && <div className="flex shrink-0 flex-col gap-2 border-b px-3 py-2">
      {offline && <Alert variant="warning"><AlertTitle>Daemon offline</AlertTitle><AlertDescription>Your draft stays here. Reconnect to save or run it.</AlertDescription></Alert>}
      {error && <Failure message={error} />}
      {notice && <p role="status" className="text-xs text-muted-foreground">{notice}</p>}
      {blocked && <p role="alert" className="text-xs text-destructive-foreground">Fix the fields marked invalid before saving.</p>}
      {summary.length > 0 && <Alert variant="error">
        <AlertTitle>{outcome === "rejected" ? `Not saved: ${issueCount === 1 ? "1 problem" : `${issueCount} problems`}` : `${issueCount === 1 ? "1 problem" : `${issueCount} problems`}; this flow cannot run until they are fixed`}</AlertTitle>
        <AlertDescription><ul className="flex flex-col gap-0.5">{summary.map((item) => <li key={item.key}>
          {item.id ? <button type="button" className="text-left underline-offset-2 hover:underline" onClick={() => { setTab("flow"); setSelected(item.id); }}>{item.text}</button> : item.text}
        </li>)}</ul></AlertDescription>
      </Alert>}
    </div>}
    {tab === "runs" && definition.id ? <RunsPanel key={runFocus ?? "runs"} automationId={definition.id} types={types} offline={offline} focus={runFocus} /> :
      <FlowContext value={flow}>
        <DndContext sensors={sensors} collisionDetection={collision} accessibility={{ announcements }} onDragStart={onDragStart} onDragEnd={onDragEnd} onDragCancel={() => setDragging(null)}>
          <div className="flex min-h-0 flex-1 flex-col lg:flex-row">
            <div className="min-h-0 min-w-0 flex-1 overflow-auto">
              <div className="mx-auto flex max-w-2xl flex-col p-4">
                <TriggerCard trigger={definition.trigger} onChange={(trigger) => patch({ trigger })} topics={topics} issue={flowIssue} offline={offline} invalid={markInvalid} />
                <BlockStack steps={steps} lane={ROOT} depth={0} where="the flow" />
                <details className="mt-6 rounded-lg border px-3 py-2" open={SETTINGS.some((key) => issues.flow[key]?.length) || undefined}>
                  <summary className="cursor-pointer rounded-sm text-sm font-medium outline-none focus-visible:ring-2 focus-visible:ring-ring">Flow settings</summary>
                  <div className="mt-3 flex flex-col gap-3 pb-1">
                    <TextField label="Description (optional)" multiline value={definition.description} issue={flowIssue("description")} onChange={(description) => patch({ description })} />
                    <div className="grid grid-cols-2 gap-2">
                      <NumberField label="Run timeout (s)" value={definition.timeoutSec} min={1} max={86400} issue={flowIssue("timeoutSec")} onChange={(timeoutSec) => patch({ timeoutSec })} />
                      <NumberField label="Concurrent runs" value={definition.concurrency} max={32} issue={flowIssue("concurrency")} hint="0 uses the default from Settings." onChange={(concurrency) => patch({ concurrency })} />
                    </div>
                    <LinesField label="Allowed programs" mono value={definition.allowedExecutables ?? []} issue={flowIssue("allowedExecutables")} hint="Run command and Run tests blocks may only start these executables. One per line." onChange={(allowedExecutables) => patch({ allowedExecutables })} />
                    <LinesField label="Allowed hosts" mono value={definition.allowedHosts ?? []} issue={flowIssue("allowedHosts")} hint="Call webhook blocks may only reach these hosts. One per line." onChange={(allowedHosts) => patch({ allowedHosts })} />
                    <SwitchField label="Allow plain HTTP" checked={definition.allowHttp} issue={flowIssue("allowHttp")} hint="Off keeps webhook calls on HTTPS." onChange={(allowHttp) => patch({ allowHttp })} />
                  </div>
                </details>
              </div>
            </div>
            <aside aria-label={selectedAt ? "Block settings" : "Blocks"} className="flex max-h-[50vh] shrink-0 flex-col overflow-auto border-t lg:max-h-none lg:w-80 lg:border-t-0 lg:border-l">
              {selectedAt
                ? <Inspector key={selectedAt.step.id} step={selectedAt.step} spec={types.find((t) => t.type === selectedAt.step.type)} tree={steps} issues={issues.steps[selectedAt.step.id] ?? {}} trigger={definition.trigger} topics={topics}
                  onChange={(next) => setSteps(updateStep(steps, selectedAt.step.id, () => next))} onRename={(id) => rename(selectedAt.step.id, id)} onRemove={() => remove(selectedAt.step.id)} onClose={() => setSelected(null)} onInvalid={markInvalid} />
                : <BlockLibrary types={types} problem={insertProblem(steps, ROOT)} onPick={(type) => insert(ROOT, steps.length, type)} />}
            </aside>
          </div>
          <DragOverlay dropAnimation={null}>{dragging && <DragPreview id={dragging.id} data={dragging.data} types={types} steps={steps} />}</DragOverlay>
        </DndContext>
      </FlowContext>}
    <AlertDialog open={leaving} onOpenChange={(open) => { if (!open) setLeaving(false); }}>
      <AlertDialogPopup>
        <AlertDialogHeader>
          <AlertDialogTitle>Discard unsaved changes?</AlertDialogTitle>
          <AlertDialogDescription>{definition.id ? "The saved version of this flow stays as it was." : "This flow has not been saved yet."}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogClose render={<Button variant="outline" />}>Keep editing</AlertDialogClose>
          <Button variant="destructive" onClick={() => { setLeaving(false); onBack(); }}>Discard</Button>
        </AlertDialogFooter>
      </AlertDialogPopup>
    </AlertDialog>
  </div>;
}

function DragPreview({ id, data, types, steps }: { id: string; data: DragData; types: StepType[]; steps: Step[] }) {
  const step = data.kind === "block" ? locate(steps, id)?.step : null;
  const type = data.kind === "new" ? data.type : step?.type;
  if (!type) return null;
  const label = types.find((t) => t.type === type)?.label ?? type;
  return <div className="flex w-64 cursor-grabbing items-center gap-2 rounded-lg border bg-card px-2 py-1.5 shadow-lg">
    <BlockIcon type={type} className="size-3.5 shrink-0" />
    <span className="truncate text-sm font-medium">{step?.name || label}</span>
    {step && <span className="font-mono text-[11px] text-muted-foreground">{step.id}</span>}
  </div>;
}
