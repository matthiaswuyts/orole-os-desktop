import { Shell } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { toastManager } from "@/components/ui/toast";

/** Whether a URL may be opened outside the app: http(s) only. */
export function isWebURL(raw: string): boolean {
  try {
    const { protocol } = new URL(raw);
    return protocol === "https:" || protocol === "http:";
  } catch {
    return false;
  }
}

/**
 * Opens a web URL in the default browser through the desktop shell (the Go side validates it again).
 * In the plain-browser preview there is no shell, so it falls back to a new noopener tab.
 */
export async function openExternal(raw: string): Promise<void> {
  if (!isWebURL(raw)) return;
  if (!inDesktop) { window.open(raw, "_blank", "noopener,noreferrer"); return; }
  try {
    await Shell.openExternal(raw);
  } catch (e) {
    toastManager.add({ type: "error", title: "Could not open the link", description: e instanceof Error ? e.message : String(e), timeout: 8000 });
  }
}
