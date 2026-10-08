import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";
import { addNotes, createWorkspace, deleteWorkspace, moveAndResizeNotes, removeNotes, runPaletteCommand, type SavedWorkspace } from "./helpers/workspace";

async function readWorkspaces(page: Page): Promise<SavedWorkspace[]> {
  return page.evaluate(() => JSON.parse(localStorage.getItem("orole.preview.settings.ws") ?? "[]"));
}

async function audit(page: Page) {
  const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"]).analyze();
  expect(results.violations, JSON.stringify(results.violations, null, 2)).toEqual([]);
}

test.beforeEach(async ({ page }) => {
  // Browser-owned appearance preferences only. No MyGo bridge or fake service data.
  await page.addInitScript(() => {
    if (!localStorage.getItem("orole.preview.settings")) {
      localStorage.setItem("orole.preview.settings", JSON.stringify({ ascii: { enabled: false }, sound: { uiEnabled: false } }));
    }
  });
  await page.clock.setFixedTime(new Date("2026-10-08T12:00:00Z"));
  await page.goto("/");
  await expect(page.getByRole("button", { name: "Switch workspace" })).toContainText("Development");
  await page.evaluate(() => document.fonts.ready);
});

test("shell keyboard navigation, palette focus restoration and notification popover", async ({ page }) => {
  const navigation = page.getByRole("navigation", { name: "Applications" });
  await expect(navigation).toBeVisible();
  await page.keyboard.press("Control+b");
  await expect(navigation).toBeHidden();
  await page.getByRole("button", { name: "Show sidebar" }).press("Enter");
  await expect(navigation).toBeVisible();

  const search = page.getByRole("button", { name: "Search (Ctrl+K)", exact: true });
  await search.focus();
  await page.keyboard.press("Enter");
  const palette = page.getByRole("dialog", { name: "Command palette" });
  await expect(palette.getByRole("combobox")).toBeFocused();
  const selected = await palette.getByRole("combobox").getAttribute("aria-activedescendant");
  await page.keyboard.press("ArrowDown");
  await expect(palette.getByRole("combobox")).not.toHaveAttribute("aria-activedescendant", selected ?? "");
  await page.keyboard.press("Tab");
  await expect.poll(() => palette.evaluate((element) => element.contains(document.activeElement))).toBe(true);
  await page.keyboard.press("Shift+Tab");
  await page.keyboard.press("Escape");
  await expect(palette).toBeHidden();
  await expect(search).toBeFocused();

  await runPaletteCommand(page, "Go to Settings");
  await expect(page.getByRole("heading", { name: "Appearance", exact: true })).toBeVisible();
  await page.keyboard.press("Control+k");
  await palette.getByRole("combobox").fill("no-command-matches-zzzz");
  await expect(palette.getByText("No matching commands.")).toBeVisible();
  await page.keyboard.press("Escape");
  const notifications = page.getByRole("button", { name: /^Notifications,/ });
  await notifications.focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("button", { name: "Close notifications" })).toBeVisible();
  await audit(page);
  await page.keyboard.press("Escape");
  await expect(notifications).toBeFocused();
});

test("theme and density keyboard changes persist after reload", async ({ page }) => {
  await runPaletteCommand(page, "Theme: Orole Light");
  await expect(page.locator("html")).not.toHaveClass(/dark/);
  await runPaletteCommand(page, "Toggle compact density");
  await expect(page.locator("html")).toHaveAttribute("data-density", "comfortable");
  await page.reload();
  await expect(page.locator("html")).not.toHaveClass(/dark/);
  await expect(page.locator("html")).toHaveAttribute("data-density", "comfortable");
  await page.getByRole("button", { name: "Settings", exact: true }).click();
  await expect(page.getByRole("radio", { name: "Orole Light", exact: true })).toBeChecked();
  await page.getByRole("radio", { name: "Orole Midnight", exact: true }).press("Space");
  await expect(page.locator("html")).toHaveClass(/dark/);
  await page.getByRole("navigation", { name: "Settings sections" }).getByRole("button", { name: "Accessibility", exact: true }).click();
  await page.getByLabel("Reduce motion", { exact: true }).selectOption("on");
  await page.reload();
  await page.getByRole("button", { name: "Settings", exact: true }).click();
  await page.getByRole("navigation", { name: "Settings sections" }).getByRole("button", { name: "Accessibility", exact: true }).click();
  await expect(page.getByLabel("Reduce motion", { exact: true })).toHaveValue("on");
});

test("workspace add, drag, resize, notes, remove and delete survive reload", async ({ page }) => {
  const name = "Browser acceptance workspace";
  await createWorkspace(page, name);
  await addNotes(page);
  await page.getByRole("textbox", { name: "Notes", exact: true }).fill("Persist this workspace note.");
  await expect.poll(async () => (await readWorkspaces(page)).find((w) => w.name === name)?.layout.widgets[0]?.config?.text).toBe("Persist this workspace note.");
  const before = (await readWorkspaces(page)).find((w) => w.name === name)!.layout.widgets[0]!;
  await moveAndResizeNotes(page);
  await expect.poll(async () => (await readWorkspaces(page)).find((w) => w.name === name)?.layout.widgets[0]?.x).toBeGreaterThan(before.x);
  await expect.poll(async () => (await readWorkspaces(page)).find((w) => w.name === name)?.layout.widgets[0]?.h).toBeGreaterThan(before.h);
  await expect.poll(async () => (await readWorkspaces(page)).find((w) => w.name === name)?.layout.widgets[0]?.w).toBeGreaterThan(before.w);
  const saved = (await readWorkspaces(page)).find((w) => w.name === name)!.layout;
  await page.reload();
  await expect(page.getByRole("textbox", { name: "Notes", exact: true })).toHaveValue("Persist this workspace note.");
  expect((await readWorkspaces(page)).find((w) => w.name === name)!.layout).toEqual(saved);
  await removeNotes(page);
  await expect.poll(async () => (await readWorkspaces(page)).find((w) => w.name === name)?.layout.widgets.length).toBe(0);
  await page.reload();
  await expect(page.getByRole("region", { name: "Notes", exact: true })).toHaveCount(0);
  await deleteWorkspace(page, name);
  await page.reload();
  expect((await readWorkspaces(page)).some((w) => w.name === name)).toBe(false);
});

for (const theme of ["Orole Midnight", "Orole Light"]) {
  test(`${theme}: shell, settings, palette and widget visual and axe baseline`, async ({ page }) => {
    await runPaletteCommand(page, `Theme: ${theme}`);
    await audit(page);
    await expect(page).toHaveScreenshot(`${theme.toLowerCase().replaceAll(" ", "-")}-shell.png`);
    await page.getByRole("button", { name: "Settings", exact: true }).click();
    await audit(page);
    await expect(page).toHaveScreenshot(`${theme.toLowerCase().replaceAll(" ", "-")}-appearance.png`);
    await page.keyboard.press("Control+k");
    await page.getByRole("dialog", { name: "Command palette" }).getByRole("combobox").fill("Theme:");
    await audit(page);
    await expect(page).toHaveScreenshot(`${theme.toLowerCase().replaceAll(" ", "-")}-palette.png`);
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog", { name: "Command palette" })).toBeHidden();
    await createWorkspace(page, "Visual workspace");
    await addNotes(page);
    await page.getByRole("textbox", { name: "Notes", exact: true }).fill("A quiet place for workspace notes.");
    await audit(page);
    await expect(page.getByRole("region", { name: "Notes", exact: true })).toHaveScreenshot(`${theme.toLowerCase().replaceAll(" ", "-")}-notes-widget.png`);
  });
}
