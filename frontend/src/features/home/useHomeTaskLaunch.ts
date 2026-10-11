import { useRef, useState } from "react";
import { Agents, type Mode, type StartSpec } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { useAgents } from "@/stores/agents";
import { useBoxes } from "@/stores/boxes";
import { useProjects } from "@/stores/projects";
import { useHarnessOptions } from "@/features/agents/useHarnessOptions";
import { newKey } from "@/features/agents/format";
import { toastError } from "@/features/projects/dialogs";
import { openTask } from "@/features/projects/tasks";
import { modeSupport, type PermissionMode } from "@/features/tasks/permissionModes";
import { useTaskAttachments } from "./useTaskAttachments";
import { loadProviderPreferences, saveProviderPreferences } from "./provider-preferences";

export interface TaskLaunchOptions {
  initialProjectId?: string | null;
  initialPrompt?: string;
  onStarted?: () => void;
  onBusyChange?: (busy: boolean) => void;
}

const PERMISSION_KEY = "orole.task.permissionMode";

/** The remembered default. YOLO is never remembered: it must be chosen, and confirmed, for each launch. */
function savedPermissionMode(): PermissionMode {
  try { return localStorage.getItem(PERMISSION_KEY) === "auto" ? "auto" : "manual"; } catch { return "manual"; }
}

export function useHomeTaskLaunch({ initialProjectId, initialPrompt = "", onStarted, onBusyChange }: TaskLaunchOptions = {}) {
  const projects = useProjects((s) => s.projects);
  const selectedId = useProjects((s) => s.selectedId);
  const worktrees = useProjects((s) => s.worktrees);
  const loaded = useProjects((s) => s.loaded);
  const machines = useBoxes((s) => s.machines);
  const observations = useBoxes((s) => s.observations);
  const now = useBoxes((s) => s.now);
  const [machine, setMachine] = useState(() => projects.find((p) => p.id === initialProjectId)?.machineId ?? "local");
  const source = useHarnessOptions(machine);
  const [savedProject] = useState(() => localStorage.getItem("orole.home.project"));
  const [remoteProject, setRemoteProject] = useState(initialProjectId ?? "");
  const registered = projects.filter((p) => p.machineId === machine).map((p) => ({ ...p, registered: true }));
  const choices = [...registered, ...(source.options?.projects ?? []).filter((p) => p.machine === machine && !registered.some((r) => r.id === p.id || r.path === p.path)).map((p) => ({ id: p.id, name: p.name, path: p.path, machineId: p.machine, isGit: p.git, missing: false, registered: false }))];
  const project = choices.find((p) => p.id === (remoteProject || selectedId || savedProject)) ?? choices.find((p) => !p.missing);
  const [worktreeChoice, setWorktreeChoice] = useState<{ project: string; value: string } | null>(null);
  const canIsolate = Boolean(project?.isGit && source.options?.worktree);
  const trees = project ? worktrees[project.id] ?? [] : [];
  const preferredTree = worktreeChoice?.project === project?.id ? worktreeChoice?.value : undefined;
  const tree = preferredTree && trees.find((w) => w.id === preferredTree && !w.error);
  const worktree = tree ? tree.id : preferredTree === "current" ? "current" : canIsolate ? "new" : "current";
  const [kind, setKind] = useState(() => localStorage.getItem("orole.task.harness") ?? "");
  const harnesses = (source.options?.harnesses ?? []).filter((h) => machine === "local" ? h.structured && h.modes.includes("structured") : h.modes.includes("process"));
  const harness = harnesses.find((h) => h.id === kind && h.available) ?? harnesses.find((h) => h.available);
  const [preferences, setPreferences] = useState(loadProviderPreferences);
  const selected = (harness && preferences[harness.id]) || { model: "", reasoning: "" };
  const models = harness?.models ?? [];
  const model = models.some((m) => m.id === selected.model) ? selected.model : models.find((m) => m.default)?.id || models[0]?.id || "";
  const modelOption = models.find((m) => m.id === model);
  const reasoningLevels = modelOption ? modelOption.reasoning : harness?.reasoning ?? [];
  const reasoning = reasoningLevels.includes(selected.reasoning) ? selected.reasoning : "";
  const [chosenMode, setChosenMode] = useState<PermissionMode>(savedPermissionMode);
  const [yoloConfirmed, setYoloConfirmed] = useState(false);
  // A mode this agent cannot run (or that was turned off) falls back to Manual rather than launching in it.
  const permissionSupport = harness?.permissionModes ?? [];
  const permissionMode: PermissionMode = modeSupport(permissionSupport, chosenMode).available && (chosenMode !== "yolo" || yoloConfirmed) ? chosenMode : "manual";
  const [prompt, setPrompt] = useState(initialPrompt);
  const [busy, setBusy] = useState(false);
  const attachments = useTaskAttachments(inDesktop && !busy && !source.offline);
  const inFlight = useRef(false);
  const attempt = useRef<{ signature: string; key: string } | null>(null);
  const blocked = !inDesktop ? "Open the desktop app to start a task."
    : source.offline ? "The daemon is offline."
    : !loaded ? "Loading projects…"
    : source.loading ? "Discovering agents on this system…"
    : source.error || source.options?.unavailableReason || (!project ? "Select or create a project on this system." : project.missing ? "This project's folder is missing." : !harness ? "No supported agent is installed on this system." : "");
  const canStart = !blocked && (Boolean(prompt.trim()) || attachments.ids.length > 0) && attachments.ready && !busy;
  function chooseProject(id: string) {
    const p = choices.find((p) => p.id === id);
    if (!p) return;
    setRemoteProject(id);
    if (p.registered) useProjects.getState().select(id);
    localStorage.setItem("orole.home.project", id);
    setWorktreeChoice(null);
  }
  function choosePermissionMode(mode: PermissionMode, confirmYolo: boolean) {
    setChosenMode(mode);
    setYoloConfirmed(mode === "yolo" && confirmYolo);
    if (mode === "yolo") return;
    try { localStorage.setItem(PERMISSION_KEY, mode); } catch (error) { toastError("Could not save the permission mode", error); }
  }
  function chooseMachine(id: string) {
    setMachine(id); setRemoteProject(""); setWorktreeChoice(null);
  }
  function chooseAgent(id: string, nextModel?: string, nextReasoning?: string) {
    setKind(id);
    try {
      localStorage.setItem("orole.task.harness", id);
      if (nextModel === undefined && nextReasoning === undefined) return;
      const previous = preferences[id] ?? { model: "", reasoning: "" };
      const next = { ...preferences, [id]: { model: nextModel ?? previous.model, reasoning: nextReasoning ?? previous.reasoning } };
      setPreferences(next);
      saveProviderPreferences(next);
    } catch (error) { toastError("Could not save agent preferences", error); }
  }
  async function start() {
    if (!canStart || !project || !harness || inFlight.current) return;
    const spec: Omit<StartSpec, "key"> = {
      kind: harness.id, machine, projectId: project.registered ? project.id : "", dir: tree ? tree.path : project.registered ? "" : project.path,
      prompt: prompt.trim(), model, reasoning, mode: (machine === "local" ? "structured" : "process") as Mode,
      attachmentIds: attachments.ids,
      newWorktree: worktree === "new", branch: "", baseRef: "", permissionMode, confirmYolo: permissionMode === "yolo", command: "", title: "", parentId: "", group: "",
    };
    const signature = JSON.stringify(spec);
    if (attempt.current?.signature !== signature) attempt.current = { signature, key: newKey() };
    inFlight.current = true; setBusy(true); onBusyChange?.(true);
    try {
      const session = await Agents.start({ ...spec, key: attempt.current.key });
      attempt.current = null;
      localStorage.setItem("orole.task.harness", harness.id);
      localStorage.setItem("orole.home.project", project.id);
      useAgents.getState().upsert(session); setPrompt(""); attachments.clear(); setChosenMode(savedPermissionMode()); setYoloConfirmed(false); openTask(session.id);
      onStarted?.();
    } catch (error) { toastError("Could not start task", error); }
    finally { inFlight.current = false; setBusy(false); onBusyChange?.(false); }
  }
  return { machine, machines, observations, now, choices, project, trees, worktree, canIsolate, harnesses, harness, model, reasoning, prompt, setPrompt, busy, blocked, canStart, chooseProject, chooseMachine, chooseAgent, start, attachments, permissionMode, permissionSupport, choosePermissionMode,
    chooseWorktree: (value: string) => { if (project) setWorktreeChoice({ project: project.id, value }); },
    createProject: () => { chooseMachine("local"); useProjects.getState().setNewProjectOpen(true); },
  };
}

export type HomeTaskLaunch = ReturnType<typeof useHomeTaskLaunch>;
