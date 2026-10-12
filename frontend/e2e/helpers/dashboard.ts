import { expect, type Page } from "@playwright/test";

export interface SavedWidget { id: string; type: string; x: number; y: number; w: number; h: number; config?: Record<string, unknown> }

/** Widgets the browser preview persisted under the `dashboard` setting. */
export function readDashboard(page: Page): Promise<SavedWidget[]> {
  return page.evaluate(() => (JSON.parse(localStorage.getItem("orole.preview.settings") ?? "{}").dashboard?.widgets ?? []) as SavedWidget[]);
}

/** Start from an empty Home dashboard so the Notes widget under test is the only one. */
export async function emptyDashboard(page: Page) {
  await page.evaluate(() => {
    const settings = JSON.parse(localStorage.getItem("orole.preview.settings") ?? "{}");
    localStorage.setItem("orole.preview.settings", JSON.stringify({ ...settings, dashboard: { widgets: [] } }));
  });
  await page.reload();
}

export async function addNotes(page: Page) {
  await page.getByRole("button", { name: "Customize", exact: true }).press("Enter");
  await page.getByRole("button", { name: /^Notes Free-form notes/ }).press("Enter");
  await page.keyboard.press("Escape");
  await expect(page.getByRole("region", { name: "Notes", exact: true })).toBeVisible();
}

/** Exercise the real grid's pointer handlers, not store setters or synthetic layout callbacks. */
export async function moveAndResizeNotes(page: Page) {
  const card = page.getByRole("region", { name: "Notes", exact: true });
  await card.scrollIntoViewIfNeeded();
  const header = card.locator(".widget-handle");
  const box = await header.boundingBox();
  if (!box) throw new Error("Notes drag handle has no rendered bounds");
  await page.mouse.move(box.x + 70, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + 250, box.y + box.height / 2, { steps: 16 });
  await page.mouse.up();
  const handle = card.locator("..").locator(".react-resizable-handle");
  await handle.scrollIntoViewIfNeeded();
  const resize = await handle.boundingBox();
  if (!resize) throw new Error("Notes resize handle has no rendered bounds");
  await page.mouse.move(resize.x + resize.width / 2, resize.y + resize.height / 2);
  await page.mouse.down();
  await page.mouse.move(resize.x + resize.width / 2 + 90, resize.y + resize.height / 2 + 46, { steps: 12 });
  await page.mouse.up();
}

export async function removeNotes(page: Page) {
  const menu = page.getByRole("button", { name: "Notes options", exact: true });
  await menu.focus();
  await page.keyboard.press("Enter");
  await page.getByRole("menuitem", { name: "Remove widget", exact: true }).press("Enter");
  await expect(page.getByRole("region", { name: "Notes", exact: true })).toHaveCount(0);
}

export async function runPaletteCommand(page: Page, title: string) {
  await page.keyboard.press("Control+k");
  const palette = page.getByRole("dialog", { name: "Command palette" });
  const input = palette.getByRole("combobox");
  await expect(input).toBeFocused();
  await input.fill(title);
  await expect(palette.getByRole("option").first()).toContainText(title);
  await input.press("Enter");
  await expect(palette).toBeHidden();
}
