/**
 * Last-known daemon data, kept in localStorage so a settings page can paint instantly on open and refresh in the
 * background. The daemon stays the source of truth: callers must still reload, and must never put secrets here.
 */
const PREFIX = "orole.snapshot.v1.";

export function readSnapshot<T>(key: string): T | undefined {
  try {
    const raw = localStorage.getItem(PREFIX + key);
    return raw === null ? undefined : (JSON.parse(raw) as T);
  } catch {
    return undefined;
  }
}

export function writeSnapshot(key: string, value: unknown): void {
  try {
    localStorage.setItem(PREFIX + key, JSON.stringify(value));
  } catch {
    // Storage full or unavailable: the cache is an optimisation only.
  }
}
