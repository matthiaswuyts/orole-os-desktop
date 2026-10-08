import type { Session } from "@/mygo";
import { isRemoteProcess } from "@/features/agents/format";

export type ComposerState =
  | { canSend: false; reason: string }
  | { canSend: true; delivery: "prompt" | "queue"; placeholder: string; label: string; note?: string };

/** Whether the harness can take a message right now, and if not, why (shown instead of the input). */
export function composerState(session: Session): ComposerState {
  const remote = isRemoteProcess(session);
  if (remote && session.status === "lost") return { canSend: false, reason: "The remote process exit is unconfirmed. Inspect the box and verify that the previous process stopped before starting a new task." };
  if (session.mode === "terminal" && session.live) return { canSend: false, reason: "This session runs in a terminal. Open Terminal in the titlebar to continue; its foreground process owns the input." };
  if (remote && session.live) return { canSend: false, reason: "This SSH process reports output and exit status only. It cannot receive messages while running." };
  if (remote) return { canSend: true, delivery: "prompt", placeholder: "New task on this box…", label: "Start task", note: "Starts a fresh task on the same box with this session's model and reasoning. It does not resume the previous conversation." };
  if (session.mode === "structured" && session.live && (session.status === "working" || session.status === "needs-you" || !!session.queue?.length)) {
    return {
      canSend: true, delivery: "queue", placeholder: "Queue a follow-up…", label: "Queue message",
      note: session.questions?.length ? "Answer the agent's questions in the question card. Messages here join the follow-up queue." : "Follow-ups wait in the daemon's queue. They do not interrupt the current turn.",
    };
  }
  return { canSend: true, delivery: "prompt", placeholder: session.live ? "Ask, Search or Chat…" : "Resume with a message…", label: "Send" };
}
