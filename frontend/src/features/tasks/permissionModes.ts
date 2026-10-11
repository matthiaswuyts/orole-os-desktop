import type { PermissionModeSupport } from "@/mygo";

/** The modes a person can pick. The daemon also has a read-only `plan` for its own reviewer sessions. */
export type PermissionMode = "manual" | "auto" | "yolo";

/** Stored sessions may carry "" or Claude Code's old "default"; both mean Manual. `plan` is read-only and not selectable. */
export function normalizePermissionMode(value: string | undefined | null): PermissionMode | "plan" {
  return value === "auto" || value === "yolo" || value === "plan" ? value : "manual";
}

/** What the harness says about one mode; without a report only Manual is offered. */
export function modeSupport(support: readonly PermissionModeSupport[] | undefined, mode: PermissionMode): { available: boolean; approximated: boolean; detail: string } {
  const found = support?.find((entry) => entry.mode === mode);
  if (found) return { available: found.available, approximated: found.approximated, detail: found.detail };
  return mode === "manual" ? { available: true, approximated: false, detail: "" } : { available: false, approximated: false, detail: "This agent keeps its own approvals." };
}
