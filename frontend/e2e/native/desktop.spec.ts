import { test, expect, nativeCall } from "./fixture";
import { addClock, addNotes, moveAndResizeNotes, removeNotes, runPaletteCommand, type SavedWidget } from "../helpers/dashboard";
import type { NotificationList, Prefs } from "../../src/mygo";

test("installed dashboard grid and palette persist through the real daemon", async ({ nativePage: page }) => {
  const settings = await nativeCall<Record<string, unknown>>(page, "Settings.All");
  const widgets = async () => ((await nativeCall<{ dashboard?: { widgets: SavedWidget[] } }>(page, "Settings.All")).dashboard?.widgets ?? []);
  try {
    await nativeCall(page, "Settings.Set", "dashboard", { widgets: [] });
    await page.reload();
    await addNotes(page);
    await addClock(page);
    const notes = async () => (await widgets()).find((w) => w.type === "notes");
    await page.getByRole("textbox", { name: "Notes", exact: true }).fill("Saved by the installed WebView2 application.");
    await expect.poll(async () => (await notes())?.config?.text).toBe("Saved by the installed WebView2 application.");
    await moveAndResizeNotes(page);
    await expect.poll(async () => (await widgets()).map((w) => w.type)).toEqual(["clock", "notes"]);
    await expect.poll(async () => (await notes())?.size).toBe("l");
    const saved = await widgets();
    await runPaletteCommand(page, "Theme: Orole Light");
    await expect.poll(async () => (await nativeCall<{ appearance?: { theme: string } }>(page, "Settings.All")).appearance?.theme).toBe("light");
    await page.reload();
    await expect(page.getByRole("textbox", { name: "Notes", exact: true })).toHaveValue("Saved by the installed WebView2 application.");
    await expect(page.locator("html")).not.toHaveClass(/dark/);
    expect(await widgets()).toEqual(saved);
    await removeNotes(page);
    await expect.poll(async () => (await widgets()).map((w) => w.type)).toEqual(["clock"]);
    await page.reload();
    await expect(page.getByRole("region", { name: "Notes", exact: true })).toHaveCount(0);
  } finally {
    await nativeCall(page, "Settings.Set", "appearance", settings.appearance ?? { theme: "midnight", density: "compact", customTokens: {} });
    await nativeCall(page, "Settings.Set", "dashboard", settings.dashboard ?? { widgets: [] });
    await page.reload();
  }
});

test("installed notification center marks a test read and keeps it through reload", async ({ nativePage: page }) => {
  const original = await nativeCall<Prefs>(page, "Notifications.Prefs");
  try {
    await nativeCall(page, "Notifications.SetPrefs", { kinds: { ...original.kinds, message: { inApp: true, sound: false } } });
    const result = await nativeCall<{ stored: boolean; notification: NotificationList["items"][number] }>(page, "Notifications.Test");
    expect(result.stored).toBe(true);
    const notification = result.notification;
    // Newest first, so the first row with the title is this run's test.
    const row = (unread: boolean) => page.getByRole("list", { name: "Notifications", exact: true })
      .getByRole("button", { name: new RegExp(`^${notification.title}, Message, .+${unread ? ", unread$" : "(?<!, unread)$"}`) }).first();
    await page.getByRole("button", { name: /^Notifications,/ }).click();
    await row(true).click();
    await expect.poll(async () => (await nativeCall<NotificationList>(page, "Notifications.List", false)).items.find((item) => item.id === notification.id)?.read).toBe(true);
    await page.reload();
    await page.getByRole("button", { name: /^Notifications,/ }).click();
    await expect(row(false)).toBeVisible();
    await page.getByRole("tab", { name: /^Needs you/ }).click();
    await expect(page.getByText("Nothing needs you")).toBeVisible();
  } finally {
    await nativeCall(page, "Notifications.SetPrefs", original);
    await page.reload();
  }
});
