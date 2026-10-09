/** A bare key is a shortcut only outside fields and popups, with no modifier, and while no dialog or sheet is open. */
export function isShortcutTarget(event: KeyboardEvent): boolean {
  if (event.ctrlKey || event.metaKey || event.altKey) return false;
  const el = event.target instanceof HTMLElement ? event.target : null;
  if (el && (el.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(el.tagName) || el.closest("[role=combobox], [role=listbox]"))) return false;
  return !document.querySelector("[role=dialog], [role=alertdialog]");
}
