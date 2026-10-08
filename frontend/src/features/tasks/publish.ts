import type { PublishSpec, Session } from "@/mygo";
import { isRemoteProcess, sessionTitle } from "@/features/agents/format";

export type PublishStep = "commit" | "push" | "pr";
export type StepState = "pending" | "running" | "done" | "failed";

/** How far a publish should go: each goal runs every step before it. */
export type PublishGoal = PublishStep;

export interface PublishForm { message: string; title: string; body: string; base: string; draft: boolean }

export const STEP_LABEL: Record<PublishStep, string> = { commit: "Commit changes", push: "Push branch", pr: "Open pull request" };
export const STEP_FAILURE: Record<PublishStep, string> = { commit: "Could not commit the changes", push: "Could not push the branch", pr: "Could not open the pull request" };

const ORDER: readonly PublishStep[] = ["commit", "push", "pr"];

/** The steps a goal runs, in order. */
export const stepsFor = (goal: PublishGoal): PublishStep[] => ORDER.slice(0, ORDER.indexOf(goal) + 1);

/** Whether the goal sends anything off the machine, so the user must confirm it first (Go enforces this too). */
export const needsConfirmation = (goal: PublishGoal): boolean => goal !== "commit";

/** Why publishing is unavailable for this session, or null when it can proceed. */
export function publishBlocker(session: Session): string | null {
  if (isRemoteProcess(session)) return "SSH process sessions have no local worktree to publish.";
  if (!session.dir) return "This session has no working directory, so there is nothing to commit.";
  if (!session.branch) return "This session has no branch. Start the task in a new worktree to publish its work.";
  return null;
}

/** The pull request the confirm dialog shows, with the same defaults the daemon applies. */
export function pullRequestPlan(session: Session, form: PublishForm) {
  return { title: form.title.trim() || sessionTitle(session), base: form.base.trim() || "the repository's default branch", draft: form.draft };
}

/**
 * The daemon runs one call as commit -> push -> PR and stops at the first failure. Calling it once
 * per step (each skips work that is already done) lets the panel show real per-step progress and
 * attribute an error to its step. `confirmed` is the user's answer to the confirm dialog and is only
 * sent on the push and PR steps; commit-only needs none.
 */
export function specFor(step: PublishStep, form: PublishForm, confirmed: boolean): PublishSpec {
  const none = { title: "", body: "", base: "", draft: false };
  switch (step) {
    case "commit": return { message: form.message.trim(), push: false, pr: false, confirmed: false, ...none };
    case "push": return { message: "", push: true, pr: false, confirmed, ...none };
    case "pr": return { message: "", push: true, pr: true, confirmed, title: form.title.trim(), body: form.body.trim(), base: form.base.trim(), draft: form.draft };
  }
}

/** A publish step failed; `step` says which one. */
export class PublishError extends Error {
  readonly step: PublishStep;
  constructor(step: PublishStep, cause: unknown) {
    super(cause instanceof Error ? cause.message : String(cause), { cause });
    this.name = "PublishError";
    this.step = step;
  }
}

export type PublishFn = (id: string, spec: PublishSpec) => Promise<Session>;

/**
 * Runs the goal's steps in order, reporting each step's state. Throws PublishError at the first
 * failure (later steps stay pending). Resolves with the session returned by the last step.
 */
export async function runPublish(session: Session, goal: PublishGoal, form: PublishForm, confirmed: boolean, publish: PublishFn, onStep: (step: PublishStep, state: StepState) => void): Promise<Session> {
  const steps = stepsFor(goal);
  for (const step of steps) onStep(step, "pending");
  let latest = session;
  for (const step of steps) {
    onStep(step, "running");
    try {
      latest = await publish(session.id, specFor(step, form, confirmed));
    } catch (e) {
      onStep(step, "failed");
      throw new PublishError(step, e);
    }
    onStep(step, "done");
  }
  return latest;
}
