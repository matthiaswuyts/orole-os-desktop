import { expect, type Page } from "@playwright/test";

export interface SavedWidget { id: string; type: string; size: "s" | "m" | "l" | "full"; tall?: boolean; config?: Record<string, unknown> }

/** Widgets the browser preview persisted under the `dashboard` setting. */
export function readDashboard(page: Page): Promise<SavedWidget[]> {
  return page.evaluate(() => (JSON.parse(localStorage.getItem("orole.preview.settings") ?? "{}").dashboard?.widgets ?? []) as SavedWidget[]);
}

/** Start from an empty Home dashboard so the widgets under test are the only ones. */
export async function emptyDashboard(page: Page) {
  await page.evaluate(() => {
    const settings = JSON.parse(localStorage.getItem("orole.preview.settings") ?? "{}");
    localStorage.setItem("orole.preview.settings", JSON.stringify({ ...settings, dashboard: { widgets: [] } }));
  });
  await page.reload();
}

async function addWidget(page: Page, gallery: RegExp, region: string) {
  await page.getByRole("button", { name: "Customize", exact: true }).press("Enter");
  await page.getByRole("button", { name: gallery }).press("Enter");
  await page.keyboard.press("Escape");
  await expect(page.getByRole("region", { name: region, exact: true })).toBeVisible();
}

export const addNotes = (page: Page) => addWidget(page, /^Notes Free-form notes/, "Notes");
export const addClock = (page: Page) => addWidget(page, /^Clock & calendar Current time/, "Clock & calendar");

/**
 * Exercise the real dnd-kit pointer sensor and the widget size menu, not store setters: drag the
 * Notes heading past the Clock widget added after it, then make Notes large.
 */
export async function moveAndResizeNotes(page: Page) {
  const card = page.getByRole("region", { name: "Notes", exact: true });
  await card.scrollIntoViewIfNeeded();
  const box = await card.locator(".widget-handle").boundingBox();
  const target = await page.getByRole("region", { name: "Clock & calendar", exact: true }).boundingBox();
  if (!box || !target) throw new Error("Notes or Clock has no rendered bounds");
  await page.mouse.move(box.x + 70, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(target.x + target.width / 2, target.y + target.height / 2, { steps: 16 });
  await page.mouse.up();
  // dnd-kit swallows clicks for 50ms after a pointer drop (AbstractPointerSensor detach), so the
  // menu's keyboard-synthesized click may be eaten if pressed immediately; retry until it opens.
  const menu = page.getByRole("button", { name: "Notes options", exact: true });
  const large = page.getByRole("menuitemradio", { name: "Large", exact: true });
  await expect(async () => {
    if (!(await large.isVisible())) await menu.press("Enter");
    await expect(large).toBeVisible({ timeout: 500 });
  }).toPass();
  await large.press("Enter");
  await page.keyboard.press("Escape");
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
