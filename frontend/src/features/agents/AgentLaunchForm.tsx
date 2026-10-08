import { useEffect, useId, useRef, useState } from "react";
import { ArrowUp, Box, ChevronDown, Folder, GitBranch, RefreshCw, Settings2 } from "lucide-react";
import { Agents, type LaunchOptions, type Mode, type Session, type StartSpec } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { useDaemon } from "@/stores/daemon";
import { useBoxes } from "@/stores/boxes";
import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Popover, PopoverPopup, PopoverTitle, PopoverTrigger } from "@/components/ui/popover";
import { Select, SelectGroup, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { Kbd, KbdGroup } from "@/components/ui/kbd";
import { AgentGlyph, ErrorNote } from "./shared";
import { baseName, errText, newKey, useAction } from "./format";

/** Machine changes invalidate the entire discovered option set, never just its label. */
function useLaunchOptions(machine: string) {
  const link = useDaemon((state) => state.link);
  const [revision, setRevision] = useState(0);
  const machines = useBoxes((state) => state.machines);
  const machineError = useBoxes((state) => state.error);
  const refreshBoxes = useBoxes((state) => state.refresh);
  const [result, setResult] = useState<{ machine: string; options?: LaunchOptions; error?: string } | null>(null);
  useEffect(() => { void refreshBoxes(); }, [revision, link, refreshBoxes]);
  useEffect(() => {
    if (!inDesktop) return;
    let active = true;
    setResult(null);
    void Agents.options(machine).then((options) => {
      if (active) setResult({ machine, options });
    }, (error: unknown) => { if (active) setResult({ machine, error: errText(error) }); });
    return () => { active = false; };
  }, [machine, revision, link]);
  const current = result?.machine === machine ? result : null;
  return {
    machines, options: current?.options, error: current?.error ?? machineError,
    loading: inDesktop && current === null, offline: link === "offline",
    refresh: () => setRevision((value) => value + 1),
  };
}

export function AgentLaunchForm({ onStarted, onBusyChange, requirePrompt = true }: {
  onStarted: (session: Session) => void;
  onBusyChange?: (busy: boolean) => void;
  requirePrompt?: boolean;
}) {
  const id = useId();
  const [machine, setMachine] = useState("local");
  const source = useLaunchOptions(machine);
  const [chosenDir, setDir] = useState<string | null>(null);
  const [chosenKind, setKind] = useState("");
  const [chosenMode, setMode] = useState<Mode | null>(null);
  const [prompt, setPrompt] = useState("");
  const [model, setModel] = useState("");
  const [reasoning, setReasoning] = useState("");
  const [worktreeChoice, setWorktree] = useState<boolean | null>(null);
  const [permissionMode, setPermissionMode] = useState("default");
  const [branch, setBranch] = useState("");
  const [command, setCommand] = useState("");
  const [projectOpen, setProjectOpen] = useState(false);
  const attempt = useRef<{ signature: string; key: string } | null>(null);
  const action = useAction();
  useEffect(() => { onBusyChange?.(action.busy); }, [action.busy, onBusyChange]);
  const options = source.options;
  const harness = options?.harnesses.find((item) => item.id === chosenKind)
    ?? (!chosenKind ? options?.harnesses.find((item) => item.available) : undefined);
  const kind = harness?.id ?? "";
  const dirs = [...new Map([
    ...(options?.directories ?? []).map((item) => [item.path, item] as const),
    ...(options?.projects ?? []).map((item) => [item.path, item] as const),
  ]).values()];
  const dir = chosenDir ?? options?.projects[0]?.path ?? dirs[0]?.path ?? "";
  const directory = dirs.find((item) => item.path === dir);
  const selectedMachine = source.machines.find((item) => item.id === machine);
  const mode = chosenMode && harness?.modes.includes(chosenMode) ? chosenMode : harness?.modes[0];
  const worktreeSupported = Boolean(options?.worktree && directory?.git);
  const newWorktree = worktreeSupported && (worktreeChoice ?? true);
  const selectedReasoning = harness?.reasoning.includes(reasoning) ? reasoning : "";
  const blocked = !inDesktop ? "Open the desktop app to start an agent."
    : source.offline ? "The daemon is offline. Reconnect before starting a task."
    : source.loading ? "Discovering harnesses and projects on this box…"
    : source.error ? null
    : options?.unavailableReason || (!harness?.available ? "No supported harness is installed on this box." : "")
      || (!dir.trim() ? "Choose a project or working directory." : "")
      || (kind === "custom" && !command.trim() ? "Enter the custom command in Launch settings." : "");
  const canStart = Boolean(inDesktop && !source.offline && !source.loading && !source.error
    && !blocked && harness?.available && mode && dir.trim() && (!requirePrompt || prompt.trim()) && !action.busy);
  const pickDirectory = (path: string) => { setDir(path); setWorktree(null); setBranch(""); };
  const submit = () => {
    if (!canStart || !mode) return;
    const spec: Omit<StartSpec, "key"> = {
      kind, machine, dir: dir.trim(), prompt: prompt.trim(), model: harness?.customModel ? model.trim() : "",
      reasoning: selectedReasoning, mode, command: kind === "custom" ? command.trim() : "",
      title: "", permissionMode: kind === "claude" ? permissionMode : "", newWorktree,
      branch: newWorktree ? branch.trim() : "", baseRef: "", parentId: "", group: "",
    };
    const signature = JSON.stringify(spec);
    // An uncertain transport failure must not turn Retry into a second agent process.
    if (attempt.current?.signature !== signature) attempt.current = { signature, key: newKey() };
    const key = attempt.current.key;
    void action.run(async () => {
      const session = await Agents.start({ ...spec, key });
      attempt.current = null;
      setPrompt("");
      onStarted(session);
    });
  };
  const worktreeHint = !options?.worktree ? "New worktrees are not supported on this box. The task uses the selected directory."
    : !directory?.git ? "Choose a discovered Git project to create an isolated worktree."
    : newWorktree ? "Create a new branch and isolated worktree for this task."
    : "Work directly in the selected project. Changes are not isolated.";

  return <form className="flex min-w-0 flex-col" onSubmit={(event) => { event.preventDefault(); submit(); }}>
    <Field className="p-3 pb-1">
      <FieldLabel htmlFor={`${id}-prompt`} className="sr-only">Task for your agent</FieldLabel>
      <Textarea id={`${id}-prompt`} unstyled className="w-full rounded-md focus-within:ring-2 focus-within:ring-ring [&_textarea]:max-h-64 [&_textarea]:min-h-24 [&_textarea]:resize-none"
        placeholder="Describe a task, ask a question, or plan your next change…" value={prompt} disabled={action.busy}
        aria-describedby={`${id}-hint`} onChange={(event) => setPrompt(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing && !event.altKey) {
            event.preventDefault();
            if (!event.repeat) event.currentTarget.form?.requestSubmit();
          }
        }} />
    </Field>
    <div className="flex flex-wrap items-center gap-1.5 px-3 pb-2">
      <Popover open={projectOpen} onOpenChange={setProjectOpen}>
        <PopoverTrigger render={<Button type="button" variant="ghost" size="xs" disabled={action.busy || source.loading || !options} className="max-w-full sm:max-w-56" />} aria-label={`Project: ${directory?.name || dir || "Choose project"}`}>
          <Folder /><span className="truncate">{directory?.name || baseName(dir) || "Choose project"}</span><ChevronDown />
        </PopoverTrigger>
        <PopoverPopup align="start" className="w-80 max-w-[calc(100vw-2rem)]">
          <div className="flex flex-col gap-3">
            <PopoverTitle>Project on {selectedMachine?.name ?? "selected box"}</PopoverTitle>
            {dirs.length > 0 ? <div className="flex max-h-52 flex-col gap-1 overflow-y-auto" aria-label="Available project directories">
              {dirs.map((item) => <button key={item.path} type="button" aria-pressed={dir === item.path}
                className="rounded-md px-2 py-1.5 text-left outline-none hover:bg-accent aria-pressed:bg-accent focus-visible:ring-2 focus-visible:ring-ring"
                onClick={() => { pickDirectory(item.path); setProjectOpen(false); }}>
                <span className="block text-sm font-medium">{item.name}</span><span className="block break-all text-xs text-muted-foreground">{item.path}</span>
              </button>)}
            </div> : <p className="text-xs text-muted-foreground">No projects were discovered on this box. Enter an approved working directory below.</p>}
            <Separator />
            <Field><FieldLabel htmlFor={`${id}-directory`}>Working directory</FieldLabel>
              <Input id={`${id}-directory`} value={dir} onChange={(event) => pickDirectory(event.target.value)} autoComplete="off" />
              <FieldDescription>Use a filesystem path on this box, not a project ID. The daemon checks directory access.</FieldDescription>
            </Field>
            <Button type="button" variant="outline" size="sm" onClick={() => setProjectOpen(false)}>Use directory</Button>
          </div>
        </PopoverPopup>
      </Popover>
      <Select value={machine} disabled={action.busy || !inDesktop || source.machines.length === 0} onValueChange={(value) => {
        if (!value) return;
        setMachine(value); setDir(null); setKind(""); setMode(null); setModel(""); setReasoning(""); setWorktree(null); setBranch("");
      }}>
        <SelectTrigger size="sm" className="w-auto min-w-0 max-w-48" aria-label="Box"><Box className="size-3.5 shrink-0" /><SelectValue>{selectedMachine?.name ?? "Choose box"}</SelectValue></SelectTrigger>
        <SelectPopup><SelectGroup>{source.machines.map((item) => <SelectItem key={item.id} value={item.id}>{item.name}</SelectItem>)}</SelectGroup></SelectPopup>
      </Select>
      <Tooltip>
        <TooltipTrigger render={<Button type="button" variant="ghost" size="xs" aria-pressed={newWorktree} aria-disabled={!worktreeSupported || action.busy} onClick={() => { if (worktreeSupported && !action.busy) setWorktree(!newWorktree); }} />}>
          <GitBranch />{newWorktree ? "New worktree" : "Current directory"}
        </TooltipTrigger><TooltipPopup>{worktreeHint}</TooltipPopup>
      </Tooltip>
      <Select value={kind || null} disabled={action.busy || source.loading || !options} onValueChange={(value) => {
        if (value) { setKind(value); setMode(null); setModel(""); setReasoning(""); }
      }}>
        <SelectTrigger size="sm" className="w-auto min-w-0 max-w-48" aria-label="Harness">{kind && <AgentGlyph kind={kind} />}<SelectValue placeholder="Choose harness">{harness?.name}</SelectValue></SelectTrigger>
        <SelectPopup><SelectGroup>{options?.harnesses.map((item) => <SelectItem key={item.id} value={item.id} disabled={!item.available}>
          <span>{item.name}{!item.available && <span className="block max-w-64 whitespace-normal text-xs text-muted-foreground">{item.unavailableReason || "Not installed"}</span>}</span>
        </SelectItem>)}</SelectGroup></SelectPopup>
      </Select>
    </div>
    <Separator />
    <div className="flex flex-wrap items-center gap-1.5 px-3 py-2">
      <Popover>
        <PopoverTrigger render={<Button type="button" variant="ghost" size="xs" disabled={action.busy || !harness?.customModel} className="max-w-44" />} aria-label={`Model: ${model || "Harness default"}`}>
          <span className="truncate">{model || "Default model"}</span><ChevronDown />
        </PopoverTrigger>
        <PopoverPopup align="start" className="w-72 max-w-[calc(100vw-2rem)]">
          <Field><FieldLabel htmlFor={`${id}-model`}>Custom model</FieldLabel><Input id={`${id}-model`} value={model} onChange={(event) => setModel(event.target.value)} placeholder="Harness default" autoComplete="off" />
            <FieldDescription>Enter a model ID supported by your harness and account. Leave empty to use its configured default; no model catalog is advertised by this CLI.</FieldDescription></Field>
        </PopoverPopup>
      </Popover>
      <Select value={selectedReasoning} disabled={action.busy || !harness?.reasoning.length} onValueChange={(value) => { if (value !== null) setReasoning(value); }}>
        <SelectTrigger size="sm" className="w-auto min-w-0 max-w-44" aria-label="Reasoning effort"><SelectValue>{selectedReasoning || "Default reasoning"}</SelectValue></SelectTrigger>
        <SelectPopup><SelectGroup><SelectItem value="">Harness default</SelectItem>{harness?.reasoning.map((value) => <SelectItem key={value} value={value}>{value}</SelectItem>)}</SelectGroup></SelectPopup>
      </Select>
      <Popover>
        <PopoverTrigger render={<Button type="button" size="icon-xs" variant="ghost" disabled={action.busy || !harness} />} aria-label="Launch settings"><Settings2 /></PopoverTrigger>
        <PopoverPopup align="end" className="w-72 max-w-[calc(100vw-2rem)]">
          <div className="flex flex-col gap-3"><PopoverTitle>Launch settings</PopoverTitle>
            <Field><FieldLabel htmlFor={`${id}-mode`}>Run mode</FieldLabel><Select value={mode ?? null} onValueChange={(value) => setMode(value)}><SelectTrigger id={`${id}-mode`}><SelectValue /></SelectTrigger><SelectPopup><SelectGroup>{harness?.modes.map((value) => <SelectItem key={value} value={value}>{value}</SelectItem>)}</SelectGroup></SelectPopup></Select></Field>
            {kind === "claude" && <Field><FieldLabel htmlFor={`${id}-permission`}>Permission mode</FieldLabel><Select value={permissionMode} onValueChange={(value) => { if (value) setPermissionMode(value); }}><SelectTrigger id={`${id}-permission`}><SelectValue /></SelectTrigger><SelectPopup><SelectGroup>{["default", "acceptEdits", "plan"].map((value) => <SelectItem key={value} value={value}>{value}</SelectItem>)}</SelectGroup></SelectPopup></Select></Field>}
            {kind === "custom" && <Field><FieldLabel htmlFor={`${id}-command`}>Command line</FieldLabel><Input id={`${id}-command`} value={command} onChange={(event) => setCommand(event.target.value)} /><FieldDescription>{"{prompt_file} is replaced by the task file path."}</FieldDescription></Field>}
            {newWorktree && <Field><FieldLabel htmlFor={`${id}-branch`}>Branch name</FieldLabel><Input id={`${id}-branch`} value={branch} onChange={(event) => setBranch(event.target.value)} placeholder="Generated when empty" /></Field>}
            <p className="text-xs text-muted-foreground">{machine !== "local" ? "Runs on the selected box over pinned SSH. No local terminal or worktree review is available." : mode === "terminal" ? "Interactive terminal session; approve harness requests in its terminal." : "Structured session; inspect output and approvals in the agent conversation."}</p>
          </div>
        </PopoverPopup>
      </Popover>
      <div className="ml-auto flex items-center gap-1">
        <Tooltip><TooltipTrigger render={<Button type="button" variant="ghost" size="icon-xs" disabled={!inDesktop || source.loading || action.busy} onClick={source.refresh} />} aria-label="Refresh launch options"><RefreshCw /></TooltipTrigger><TooltipPopup>Refresh boxes, projects and installed harnesses</TooltipPopup></Tooltip>
        <Tooltip><TooltipTrigger render={<Button type="submit" size="icon-sm" disabled={!canStart} />} aria-label={action.busy ? "Starting agent" : "Start task"}>{action.busy ? <Spinner /> : <ArrowUp />}</TooltipTrigger><TooltipPopup><span className="flex items-center gap-2">Start task <Kbd>Enter</Kbd></span></TooltipPopup></Tooltip>
      </div>
    </div>
    <div className="flex flex-col gap-1 px-3 pb-3">
      <p id={`${id}-hint`} className="text-xs text-muted-foreground">{action.busy ? "Starting on the selected box. Approve any request in Permissions; your task stays here until launch succeeds." : <><Kbd>Enter</Kbd> to start · <KbdGroup aria-label="Shift+Enter"><Kbd>Shift</Kbd><Kbd>Enter</Kbd></KbdGroup> for a new line</>}</p>
      {blocked && <p role="status" className="text-xs text-muted-foreground">{blocked}</p>}
      {machine !== "local" && options && !options.unavailableReason && <p className="text-xs text-muted-foreground">Remote tasks run once on this box. Follow-up messages and interactive terminals are unavailable.</p>}
      <ErrorNote error={source.error ?? null} /><ErrorNote error={action.error} />
    </div>
  </form>;
}
