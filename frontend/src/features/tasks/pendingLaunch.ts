import { Agents, type Session, type StartSpec } from "@/mygo";
import { toastManager } from "@/components/ui/toast";
import { kindLabel, newKey } from "@/features/agents/format";
import type { TaskAttachment } from "@/features/home/useTaskAttachments";
import { errText } from "@/features/projects/dialogs";
import { openTask } from "@/features/projects/tasks";
import { useAgents } from "@/stores/agents";
import { useProjects } from "@/stores/projects";
import { useUI, type ViewId } from "@/stores/ui";
import type { PermissionMode } from "./permissionModes";

/** The launcher's form as it was at Send, so "Edit prompt" can put every choice back. */
export interface LaunchDraft {
  launcher: "home" | "new-task";
  /** The New task page's own project (null: it let the person pick). */
  initialProjectId: string | null;
  /** Where the New task page returns to. */
  returnTo: ViewId;
  /** The prompt exactly as typed. */
  prompt: string;
  /** Uploads already staged in the daemon; they stay valid for a second try. */
  attachments: TaskAttachment[];
  machine: string;
  project: string;
  worktree: { project: string; value: string } | null;
  harness: string;
  /** YOLO is never carried over: it has to be confirmed again for each launch. */
  permissionMode: PermissionMode;
  /** The idempotency key of the failed attempt: an identical resend must not start a second task. */
  attempt: { signature: string; key: string } | null;
}

/** A task the person has sent whose `Agents.start` call has not returned a session yet, or failed. */
export interface PendingLaunch {
  /** `launch:<uuid>`. Doubles as the agents store's `openId` while the pending page is shown. */
  id: string;
  /** Provider id of the chosen agent. */
  kind: string;
  /** What the daemon titles the task from the prompt until a better title arrives. */
  title: string;
  prompt: string;
  attachments: TaskAttachment[];
  /** A new git worktree is created for this task. */
  worktree: boolean;
  /** Registered project the task belongs to ("" for an unregistered remote folder). */
  projectId: string;
  startedAt: number;
  status: "starting" | "failed";
  error?: string;
  /** The exact call, idempotency key included: a retry sends it again unchanged. */
  request: StartSpec;
  draft: LaunchDraft;
  /** Runs when the call settles: the started session, or null when it failed. */
  onSettled?: (session: Session | null) => void;
}

const TITLE_MAX = 80;

/** The title the daemon gives a task before any other: the first line of the prompt, else the agent's name. */
export function provisionalTitle(prompt: string, kind: string): string {
  const first = prompt.trim().split(/\r?\n/)[0] ?? "";
  if (!first) return kindLabel(kind);
  return first.length > TITLE_MAX ? `${first.slice(0, TITLE_MAX)}…` : first;
}

export interface LaunchInput {
  request: StartSpec;
  prompt: string;
  attachments: TaskAttachment[];
  projectId: string;
  draft: LaunchDraft;
  onSettled?: PendingLaunch["onSettled"];
}

/** Whether the person is still looking at this launch's page: only then does a result move them. */
function watching(id: string): boolean {
  return useAgents.getState().openId === id && useUI.getState().view === "agents";
}

/**
 * Shows the pending task page at once and runs `Agents.start` behind it. The call belongs to no component, so leaving
 * the page (or unmounting the launcher) neither cancels it nor loses its result.
 */
export function beginLaunch({ request, prompt, attachments, projectId, draft, onSettled }: LaunchInput): string {
  const id = `launch:${newKey()}`;
  useAgents.getState().addLaunch({
    id, kind: request.kind, title: provisionalTitle(request.prompt, request.kind), prompt, attachments, worktree: request.newWorktree, projectId,
    startedAt: Date.now(), status: "starting", request, draft, onSettled,
  });
  openTask(id);
  void dispatch(id);
  return id;
}

async function dispatch(id: string) {
  const launch = useAgents.getState().launches[id];
  if (!launch) return;
  let session: Session;
  try {
    session = await Agents.start(launch.request);
  } catch (error) {
    const message = errText(error);
    const away = !watching(id);
    useAgents.getState().patchLaunch(id, { status: "failed", error: message });
    if (away) toastManager.add({ type: "error", title: "Could not start task", description: message, timeout: 8000, actionProps: { children: "Review", onClick: () => openTask(id) } });
    launch.onSettled?.(null);
    return;
  }
  useAgents.getState().resolveLaunch(id, session, watching(id));
  // The daemon has taken the uploads into the task; the staged copies are spent. A failed disposal never fails the launch.
  for (const file of launch.attachments) if (file.uploadId) void Agents.discardAttachment(file.uploadId).catch(() => {});
  launch.onSettled?.(session);
}

/** Sends a failed launch again, with the same request and idempotency key. */
export function retryLaunch(id: string) {
  const launch = useAgents.getState().launches[id];
  if (launch?.status !== "failed") return;
  useAgents.getState().patchLaunch(id, { status: "starting", error: undefined });
  void dispatch(id);
}

/** Takes a failed launch off the pending page and back to the launcher it came from, with the whole draft restored. */
export function editLaunch(id: string) {
  const agents = useAgents.getState();
  const launch = agents.launches[id];
  if (launch?.status !== "failed") return;
  const { draft } = launch;
  agents.setLaunchDraft(draft);
  agents.removeLaunch(id);
  if (draft.launcher === "new-task") {
    useProjects.getState().openNewTask(draft.initialProjectId, draft.prompt);
    useProjects.setState((s) => ({ newTask: { ...s.newTask, from: draft.returnTo } }));
  } else {
    useUI.getState().setView("home");
  }
}
