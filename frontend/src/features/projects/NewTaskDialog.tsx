import { useId, useMemo, useRef, useState, type FormEvent } from "react";
import { Agents, type LaunchOptions, type Mode, type StartSpec } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { branchFor } from "@/lib/projects";
import { cn } from "@/lib/utils";
import { useAgents } from "@/stores/agents";
import { useProjects } from "@/stores/projects";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogDescription, DialogFooter, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { RadioGroup, RadioPrimitive } from "@/components/ui/radio-group";
import { Select, SelectGroup, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { ProviderLogo } from "@/features/agents/ProviderLogo";
import { newKey } from "@/features/agents/format";
import { useHarnessOptions } from "@/features/agents/useHarnessOptions";
import { toastError } from "./dialogs";
import { openTask } from "./tasks";

type Harness = LaunchOptions["harnesses"][number];

const LAST_HARNESS_KEY = "orole.task.harness";
const DEFAULT_REASONING = "__default";

/** One task = one agent session on a project folder, optionally on its own branch and worktree. */
export function NewTaskDialog() {
  const open = useProjects((s) => s.newTask.open);
  const projectId = useProjects((s) => s.newTask.projectId);
  const initialPrompt = useProjects((s) => s.newTask.prompt);
  const close = useProjects((s) => s.closeNewTask);
  const [busy, setBusy] = useState(false);
  return (
    <Dialog open={open} onOpenChange={(next) => { if (!busy && !next) close(); }}>
      <DialogPopup className="max-w-xl" showCloseButton={!busy}>
        <NewTaskForm initialProjectId={projectId} initialPrompt={initialPrompt} busy={busy} setBusy={setBusy} onDone={close} />
      </DialogPopup>
    </Dialog>
  );
}

function NewTaskForm({ initialProjectId, initialPrompt, busy, setBusy, onDone }: { initialProjectId: string | null; initialPrompt: string; busy: boolean; setBusy: (busy: boolean) => void; onDone: () => void }) {
  const id = useId();
  const projects = useProjects((s) => s.projects);
  const worktrees = useProjects((s) => s.worktrees);
  const sessions = useAgents((s) => s.sessions);
  const runnable = projects.filter((p) => !p.missing);
  const [chosenProject, setChosenProject] = useState(initialProjectId);
  const project = runnable.find((p) => p.id === chosenProject) ?? runnable[0];
  const source = useHarnessOptions(project?.machineId ?? "local");
  const [chosenKind, setKind] = useState(() => localStorage.getItem(LAST_HARNESS_KEY) ?? "");
  const [model, setModel] = useState("");
  const [reasoning, setReasoning] = useState("");
  const [prompt, setPrompt] = useState(initialPrompt);
  const [isolateChoice, setIsolate] = useState<boolean | null>(null);
  const [branchEdit, setBranchEdit] = useState<string | null>(null);
  const [suffix] = useState(() => crypto.randomUUID().slice(0, 6));
  const attempt = useRef<{ signature: string; key: string } | null>(null);

  const harnesses = (source.options?.harnesses ?? []).filter((h) => h.structured && h.modes.includes("structured"));
  const harness: Harness | undefined = harnesses.find((h) => h.id === chosenKind && h.available) ?? harnesses.find((h) => h.available);
  const mode: Mode = "structured";
  const models = harness?.models ?? [];
  const defaultModel = models.find((m) => m.default) ?? models[0];
  const selectedModel = model.trim() || defaultModel?.id || "";
  const modelOption = models.find((m) => m.id === selectedModel);
  const reasoningOptions = modelOption ? modelOption.reasoning : harness?.reasoning ?? [];
  const selectedReasoning = reasoningOptions.includes(reasoning) ? reasoning : "";
  const recentModels = useMemo(() => [...new Set(Object.values(sessions).filter((s) => s.kind === harness?.id && s.model).sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt)).map((s) => s.model))].slice(0, 8), [sessions, harness?.id]);

  const canIsolate = Boolean(source.options?.worktree && project?.isGit);
  const isolate = canIsolate && (isolateChoice ?? true);
  const generatedBranch = branchFor(prompt.trim().split(/\s+/).slice(0, 6).join(" ")) || `orole/task-${suffix}`;
  const branch = (branchEdit ?? generatedBranch).trim();
  const baseBranch = project ? worktrees[project.id]?.find((w) => w.isMain)?.branch : undefined;

  const blocked = !inDesktop ? "Open the desktop app to start a task."
    : source.offline ? "The daemon is offline. Reconnect before starting a task."
    : !project ? "Add a project to run tasks."
    : source.loading ? "Discovering installed harnesses…"
    : source.error ? source.error
    : source.options?.unavailableReason
      || (!harness ? "No structured harness is installed on this machine. Install Claude Code, Codex, or Oh My Pi to start tasks here."
        : isolate && !branch ? "Enter a branch name." : "");
  const canStart = Boolean(!blocked && project && harness && mode && prompt.trim() && !busy);

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (!canStart || !project || !harness || !mode) return;
    const spec: Omit<StartSpec, "key"> = {
      kind: harness.id, machine: project.machineId, projectId: project.id, dir: "", prompt: prompt.trim(),
      model: harness.customModel || models.length > 0 ? selectedModel : "", reasoning: selectedReasoning, mode,
      command: "", title: "", permissionMode: harness.id === "claude" ? "default" : "",
      newWorktree: isolate, branch: isolate ? branch : "", baseRef: "", parentId: "", group: "",
    };
    const signature = JSON.stringify(spec);
    // An uncertain transport failure must not turn Retry into a second agent process.
    if (attempt.current?.signature !== signature) attempt.current = { signature, key: newKey() };
    const key = attempt.current.key;
    setBusy(true);
    Agents.start({ ...spec, key }).then((session) => {
      attempt.current = null;
      localStorage.setItem(LAST_HARNESS_KEY, harness.id);
      useAgents.getState().upsert(session);
      openTask(session.id);
      onDone();
    }, (error: unknown) => toastError("Could not start task", error)).finally(() => setBusy(false));
  };

  return (
    <form onSubmit={submit} className="contents">
      <DialogHeader>
        <DialogTitle>New task</DialogTitle>
        <DialogDescription>An agent session in your project. Pick the harness it runs with; it is configured with your skills and MCP servers.</DialogDescription>
      </DialogHeader>
      <DialogPanel className="flex flex-col gap-4">
        <Field>
          <FieldLabel>Project</FieldLabel>
          <Select value={project?.id ?? null} disabled={busy || runnable.length === 0} onValueChange={(value) => { if (value) { setChosenProject(value); setIsolate(null); setBranchEdit(null); } }}>
            <SelectTrigger aria-label="Project"><SelectValue>{project?.name ?? "No project available"}</SelectValue></SelectTrigger>
            <SelectPopup><SelectGroup>{runnable.map((p) => <SelectItem key={p.id} value={p.id}>{p.name}</SelectItem>)}</SelectGroup></SelectPopup>
          </Select>
          <FieldDescription className={project?.path ? "break-all font-mono" : undefined}>{project ? project.path || "No folder needed. Orole keeps this project's files in a managed workspace." : "Create a project to run tasks. A folder is optional."}</FieldDescription>
        </Field>

        <Field>
          <FieldLabel id={`${id}-harness`}>Harness</FieldLabel>
          <RadioGroup aria-labelledby={`${id}-harness`} value={harness?.id ?? ""} onValueChange={(value) => { setKind(String(value)); setModel(""); setReasoning(""); }} className="grid w-full grid-cols-1 gap-2 sm:grid-cols-3" disabled={busy}>
            {harnesses.map((h) => (
              <RadioPrimitive.Root key={h.id} value={h.id} disabled={!h.available || busy} aria-labelledby={`${id}-harness-${h.id}`}
                className={cn("flex min-w-0 flex-col items-start gap-1 rounded-lg border px-3 py-2 text-left outline-none transition-colors hover:bg-accent/50 focus-visible:ring-2 focus-visible:ring-ring data-checked:border-primary data-checked:bg-accent data-disabled:cursor-not-allowed data-disabled:opacity-60 data-disabled:hover:bg-transparent")}>
                <span className="flex w-full min-w-0 items-center gap-2"><ProviderLogo kind={h.id} /><span id={`${id}-harness-${h.id}`} className="min-w-0 flex-1 truncate font-medium text-sm">{h.name}</span></span>
                <span className="flex flex-wrap items-center gap-1">
                  {h.available ? <>
                    {h.structured && <Badge size="sm" variant="info">Structured</Badge>}
                    {h.version && <span className="truncate font-mono text-[11px] text-muted-foreground">{h.version}</span>}
                  </> : <span className="text-muted-foreground text-xs">{h.unavailableReason || "Not installed"}</span>}
                </span>
              </RadioPrimitive.Root>
            ))}
          </RadioGroup>
          {harnesses.length === 0 && !source.loading && <FieldDescription>{source.error ?? "No harness found."}</FieldDescription>}
        </Field>

        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          {(harness?.customModel || models.length > 0) && <Field>
            <FieldLabel htmlFor={`${id}-model`}>Model</FieldLabel>
            {harness?.customModel ? <>
              <Input id={`${id}-model`} value={model} list={`${id}-models`} onChange={(e) => { setModel(e.target.value); setReasoning(""); }} placeholder={defaultModel ? `${defaultModel.name || defaultModel.id} (default)` : "Harness default"} autoComplete="off" disabled={busy} className="font-mono" />
              <datalist id={`${id}-models`}>{models.length > 0 ? models.map((m) => <option key={m.id} value={m.id}>{m.name}{m.default ? " (default)" : ""}</option>) : recentModels.map((m) => <option key={m} value={m} />)}</datalist>
            </> : <Select value={selectedModel} disabled={busy} onValueChange={(value) => { setModel(value ?? ""); setReasoning(""); }}>
              <SelectTrigger id={`${id}-model`}><SelectValue>{modelOption?.name || selectedModel}</SelectValue></SelectTrigger>
              <SelectPopup><SelectGroup>{models.map((m) => <SelectItem key={m.id} value={m.id}>{m.name || m.id}{m.default ? " (default)" : ""}</SelectItem>)}</SelectGroup></SelectPopup>
            </Select>}
          </Field>}
          {harness && reasoningOptions.length > 0 && <Field>
            <FieldLabel htmlFor={`${id}-reasoning`}>Reasoning</FieldLabel>
            <Select value={selectedReasoning || DEFAULT_REASONING} disabled={busy} onValueChange={(value) => setReasoning(!value || value === DEFAULT_REASONING ? "" : value)}>
              <SelectTrigger id={`${id}-reasoning`}><SelectValue>{selectedReasoning || "Harness default"}</SelectValue></SelectTrigger>
              <SelectPopup><SelectGroup><SelectItem value={DEFAULT_REASONING}>Harness default</SelectItem>{reasoningOptions.map((level) => <SelectItem key={level} value={level}>{level}</SelectItem>)}</SelectGroup></SelectPopup>
            </Select>
          </Field>}
        </div>

        <Field>
          <FieldLabel htmlFor={`${id}-prompt`}>Prompt</FieldLabel>
          <Textarea id={`${id}-prompt`} autoFocus value={prompt} disabled={busy} placeholder="Describe the task, ask a question, or plan your next change…" className="[&_textarea]:min-h-28"
            onChange={(e) => setPrompt(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter" && (e.ctrlKey || e.metaKey) && !e.nativeEvent.isComposing) { e.preventDefault(); e.currentTarget.form?.requestSubmit(); } }} />
          <FieldDescription>Ctrl+Enter to start.</FieldDescription>
        </Field>

        <Field>
          <div className="flex w-full items-center justify-between gap-3">
            <FieldLabel htmlFor={`${id}-isolate`}>Isolate on a new branch</FieldLabel>
            <Switch id={`${id}-isolate`} checked={isolate} disabled={!canIsolate || busy} onCheckedChange={setIsolate} />
          </div>
          {isolate ? <>
            <Input aria-label="Branch name" value={branchEdit ?? generatedBranch} onChange={(e) => setBranchEdit(e.target.value)} className="font-mono" autoComplete="off" disabled={busy} />
            <FieldDescription>Runs in its own worktree on this branch, based on <code className="font-mono">{baseBranch || "HEAD"}</code> (the project's current commit). Your checkout is not touched.</FieldDescription>
          </> : <FieldDescription>{!project ? "" : !project.path ? "Tasks share this project's managed workspace. Git branch isolation requires an attached repository." : !project.isGit ? "This folder is not a git repository: the task works directly in it." : !source.options?.worktree ? "Isolated worktrees are not available on this machine: the task works directly in the folder." : "The task works directly in the project folder. Changes are not isolated."}</FieldDescription>}
        </Field>
        {blocked && <p role="status" className="text-muted-foreground text-xs">{blocked}</p>}
      </DialogPanel>
      <DialogFooter>
        <Button type="button" variant="outline" disabled={busy} onClick={onDone}>Cancel</Button>
        <Button type="submit" disabled={!canStart}>{busy ? <><Spinner />Starting…</> : "Start task"}</Button>
      </DialogFooter>
    </form>
  );
}
