/** Elements where the native menu stays: pasting and spell-check need it and a custom menu would not replace them. */
function isEditable(target: EventTarget | null): boolean {
  if (!(target instanceof Element)) return false;
  const field = target.closest("input, textarea, [contenteditable]:not([contenteditable=false])");
  if (!field) return false;
  if (field instanceof HTMLInputElement) return !["button", "checkbox", "radio", "range", "color", "file", "image", "reset", "submit"].includes(field.type);
  return true;
}

/**
 * Turns off the browser's own right-click menu (Back, Reload, Inspect…) everywhere except editable text fields. A
 * component with its own context menu is unaffected: it opens its menu from the same event.
 * Returns a function that removes the handler.
 */
export function disableNativeContextMenu(target: Document = document): () => void {
  const onContextMenu = (event: MouseEvent) => {
    if (!isEditable(event.target)) event.preventDefault();
  };
  target.addEventListener("contextmenu", onContextMenu);
  return () => target.removeEventListener("contextmenu", onContextMenu);
}
