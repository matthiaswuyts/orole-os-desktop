import { expect, type Page } from "@playwright/test";

export interface SavedWorkspace {
  id: string;
  name: string;
  layout: { widgets: { id: string; type: string; x: number; y: number; w: number; h: number; config?: Record<string, unknown> }[] };
}

export async function openWorkspaceSettings(page: Page) {
  await page.getByRole("navigation", { name: "Applications" }).getByRole("button", { name: "Settings", exact: true }).click();
  await page.getByRole("navigation", { name: "Settings sections" }).getByRole("button", { name: "Workspaces", exact: true }).click();
}

export async function createWorkspace(page: Page, name: string) {
  await openWorkspaceSettings(page);
  await page.getByLabel("New workspace name", { exact: true }).fill(name);
  await page.getByRole("button", { name: "Create workspace", exact: true }).click();
  await expect(page.getByRole("button", { name: "Switch workspace", exact: true })).toContainText(name);
  await page.getByRole("button", { name: "Home", exact: true }).click();
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

export async function deleteWorkspace(page: Page, name: string) {
  await openWorkspaceSettings(page);
  await page.getByRole("button", { name: `Delete ${name}`, exact: true }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Delete workspace", exact: true }).click();
  await expect(page.getByRole("button", { name: `Delete ${name}`, exact: true })).toHaveCount(0);
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
