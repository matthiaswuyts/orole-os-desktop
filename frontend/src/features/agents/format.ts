import { useCallback, useEffect, useRef, useState } from "react";
import type { Session } from "@/mygo";

/** Keep backend error text intact, including its error code when present. */
export const errText = (e: unknown) => e instanceof Error ? e.message : String(e);

export const KIND_LABEL: Record<string, string> = {
  claude: "Claude Code",
  codex: "Codex",
  opencode: "OpenCode",
  gemini: "Gemini CLI",
  custom: "Custom CLI",
};

export const kindLabel = (kind: string) => KIND_LABEL[kind] ?? kind;

/** SSH process sessions have output-only live transport, not a local CLI conversation. */
export const isRemoteProcess = (session: Session) => session.mode === "process" && Boolean(session.machine && session.machine !== "local");

/** Last path segment, either separator. */
export function baseName(path: string): string {
  const parts = path.split(/[\\/]+/).filter(Boolean);
  return parts[parts.length - 1] ?? path;
}

/** Session title, falling back to the first line of its task. */
export function sessionTitle(s: Session): string {
  if (s.title.trim()) return s.title.trim();
  const first = s.task.trim().split(/\r?\n/)[0] ?? "";
  if (first) return first.length > 80 ? `${first.slice(0, 79)}…` : first;
  return `${kindLabel(s.kind)} session`;
}

/** Folder the session works on: the project's folder name, else the working directory's. */
export const folderOf = (s: Session) => baseName(s.project || s.dir);

/** 42s, 7m, 3h 05m, 2d 4h. */
export function duration(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ${String(m % 60).padStart(2, "0")}m`;
  return `${Math.floor(h / 24)}d ${h % 24}h`;
}

/** "just now", "5m ago", "3h ago", "2d ago". */
export function ago(iso: string, now = Date.now()): string {
  const s = Math.max(0, Math.floor((now - Date.parse(iso)) / 1000));
  if (!Number.isFinite(s) || s < 45) return "just now";
  if (s < 3600) return `${Math.max(1, Math.round(s / 60))}m ago`;
  if (s < 86400) return `${Math.round(s / 3600)}h ago`;
  return `${Math.round(s / 86400)}d ago`;
}

// One clock for every card, so a board of them re-renders once a second at most.
const listeners = new Set<(n: number) => void>();
let timer: number | undefined;

/** Current time in ms, ticking once a second while any component uses it. */
export function useNow(): number {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    listeners.add(setNow);
    timer ??= window.setInterval(() => listeners.forEach((l) => l(Date.now())), 1000);
    return () => {
      listeners.delete(setNow);
      if (listeners.size === 0) { clearInterval(timer); timer = undefined; }
    };
  }, []);
  return now;
}

/**
 * Runs one async action at a time: a second call while one is pending is ignored (double-click
 * guard), and the failure message is kept for display.
 */
export function useAction() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const guard = useRef(false);
  const run = useCallback(async <T>(fn: () => Promise<T>): Promise<T | undefined> => {
    if (guard.current) return undefined;
    guard.current = true;
    setBusy(true);
    setError(null);
    try {
      return await fn();
    } catch (e) {
      setError(errText(e));
      return undefined;
    } finally {
      guard.current = false;
      setBusy(false);
    }
  }, []);
  return { busy, error, run, setError };
}

/** A fresh idempotency key for one dispatching call. */
export const newKey = () => crypto.randomUUID();
