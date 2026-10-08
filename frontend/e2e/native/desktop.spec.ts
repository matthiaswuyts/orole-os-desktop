import { randomUUID } from "node:crypto";
import { test, expect, nativeCall } from "./fixture";
import { addNotes, createWorkspace, deleteWorkspace, moveAndResizeNotes, removeNotes, runPaletteCommand, type SavedWorkspace } from "../helpers/workspace";
import type { NotificationList, Prefs } from "../../src/mygo";

test("installed workspace grid and palette persist through the real daemon", async ({ nativePage: page }) => {
  const name = `CDP acceptance ${randomUUID().slice(0, 8)}`;
  const settings = await nativeCall<Record<string, unknown>>(page, "Settings.All");
  const originalActive = await page.evaluate(() => localStorage.getItem("orole.activeWorkspace"));
  let workspaceId: string | undefined;
  try {
    await createWorkspace(page, name);
    workspaceId = (await nativeCall<SavedWorkspace[]>(page, "Workspaces.List")).find((workspace) => workspace.name === name)?.id;
    expect(workspaceId).toBeTruthy();
    await addNotes(page);
    await page.getByRole("textbox", { name: "Notes", exact: true }).fill("Saved by the installed WebView2 application.");
    await expect.poll(async () => (await nativeCall<SavedWorkspace[]>(page, "Workspaces.List")).find((w) => w.id === workspaceId)?.layout.widgets[0]?.config?.text).toBe("Saved by the installed WebView2 application.");
    const before = (await nativeCall<SavedWorkspace[]>(page, "Workspaces.List")).find((w) => w.id === workspaceId)!.layout.widgets[0]!;
    await moveAndResizeNotes(page);
    await expect.poll(async () => (await nativeCall<SavedWorkspace[]>(page, "Workspaces.List")).find((w) => w.id === workspaceId)?.layout.widgets[0]?.x).toBeGreaterThan(before.x);
    await expect.poll(async () => (await nativeCall<SavedWorkspace[]>(page, "Workspaces.List")).find((w) => w.id === workspaceId)?.layout.widgets[0]?.h).toBeGreaterThan(before.h);
    await expect.poll(async () => (await nativeCall<SavedWorkspace[]>(page, "Workspaces.List")).find((w) => w.id === workspaceId)?.layout.widgets[0]?.w).toBeGreaterThan(before.w);
    const saved = (await nativeCall<SavedWorkspace[]>(page, "Workspaces.List")).find((w) => w.id === workspaceId)!.layout;
    await runPaletteCommand(page, "Theme: Orole Light");
    await expect.poll(async () => (await nativeCall<{ appearance?: { theme: string } }>(page, "Settings.All")).appearance?.theme).toBe("light");
    await page.reload();
    await expect(page.getByRole("textbox", { name: "Notes", exact: true })).toHaveValue("Saved by the installed WebView2 application.");
    await expect(page.locator("html")).not.toHaveClass(/dark/);
    expect((await nativeCall<SavedWorkspace[]>(page, "Workspaces.List")).find((w) => w.id === workspaceId)!.layout).toEqual(saved);
    await removeNotes(page);
    await expect.poll(async () => (await nativeCall<SavedWorkspace[]>(page, "Workspaces.List")).find((w) => w.id === workspaceId)?.layout.widgets.length).toBe(0);
    await page.reload();
    await expect(page.getByRole("region", { name: "Notes", exact: true })).toHaveCount(0);
    await deleteWorkspace(page, name);
    await page.reload();
    expect((await nativeCall<SavedWorkspace[]>(page, "Workspaces.List")).some((w) => w.id === workspaceId)).toBe(false);
  } finally {
    // Even a failure after create but before capturing its ID cleans up only this run's workspace.
    const owned = (await nativeCall<SavedWorkspace[]>(page, "Workspaces.List")).filter((w) => w.name === name);
    for (const workspace of owned) await nativeCall(page, "Workspaces.Delete", workspace.id);
    await nativeCall(page, "Settings.Set", "appearance", settings.appearance ?? { theme: "midnight", density: "compact", customTokens: {} });
    await page.evaluate((active) => { if (active) localStorage.setItem("orole.activeWorkspace", active); else localStorage.removeItem("orole.activeWorkspace"); }, originalActive);
    await page.reload();
  }
});

test("installed notification preferences, read and dismiss survive reload", async ({ nativePage: page }) => {
  const original = await nativeCall<Prefs>(page, "Notifications.Prefs");
  const initial = await nativeCall<NotificationList>(page, "Notifications.List", "system.test", false);
  const existing = new Set(initial.items.map((item) => item.id));
  try {
    await runPaletteCommand(page, "Go to Settings");
    await page.getByRole("navigation", { name: "Settings sections" }).getByRole("button", { name: "Notifications", exact: true }).click();
    const windows = page.getByRole("group", { name: "system.test", exact: true }).getByRole("switch", { name: /Windows notifications$/ });
    await expect(windows).toBeEnabled();
    if (await windows.isChecked()) await windows.click();
    const sound = page.getByRole("group", { name: "system.test", exact: true }).getByRole("switch", { name: /notification sound$/ });
    await expect(sound).toBeEnabled();
    if (await sound.isChecked()) await sound.click();
    await expect.poll(async () => (await nativeCall<Prefs>(page, "Notifications.Prefs")).sources["system.test"]?.native).toBe(false);
    await page.getByRole("button", { name: "Test notification", exact: true }).click();
    let created: NotificationList["items"] = [];
    await expect.poll(async () => {
      created = (await nativeCall<NotificationList>(page, "Notifications.List", "system.test", false)).items.filter((item) => !existing.has(item.id));
      return created.length;
    }).toBe(1);
    const notification = created[0]!;
    await page.getByRole("button", { name: /^Notifications,/ }).click();
    const history = page.getByRole("list", { name: "Notification history" });
    await expect(history.getByText(notification.title, { exact: true })).toBeVisible();
    await history.getByRole("button", { name: `Mark read: ${notification.title}`, exact: true }).click();
    await expect.poll(async () => (await nativeCall<NotificationList>(page, "Notifications.List", "system.test", false)).items.find((item) => item.id === notification.id)?.read).toBe(true);
    await page.reload();
    await page.getByRole("button", { name: /^Notifications,/ }).click();
    await expect(history.getByText(notification.title, { exact: true })).toBeVisible();
    await expect(history.getByRole("button", { name: `Mark read: ${notification.title}`, exact: true })).toHaveCount(0);
    await history.getByRole("button", { name: `Dismiss: ${notification.title}`, exact: true }).click();
    await expect(history.getByText(notification.title, { exact: true })).toHaveCount(0);
    await page.reload();
    expect((await nativeCall<NotificationList>(page, "Notifications.List", "system.test", false)).items.some((item) => item.id === notification.id)).toBe(false);
    await runPaletteCommand(page, "Go to Settings");
    await page.getByRole("navigation", { name: "Settings sections" }).getByRole("button", { name: "Notifications", exact: true }).click();
    await expect(windows).not.toBeChecked();
    await expect(sound).not.toBeChecked();
  } finally {
    const owned = (await nativeCall<NotificationList>(page, "Notifications.List", "system.test", false)).items.filter((item) => !existing.has(item.id));
    if (owned.length) await nativeCall(page, "Notifications.Dismiss", owned.map((item) => item.id), "", false);
    await nativeCall(page, "Notifications.SetPrefs", original);
    await page.reload();
  }
});
