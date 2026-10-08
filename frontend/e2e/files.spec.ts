import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

interface HarnessGlobal { __filesHarness: { externalChange: () => void; savedText: () => string } }

/** The desktop bridge and all fixture content exist only inside this browser harness. */
async function installFilesBridge(page: Page) {
  await page.addInitScript(() => {
    type Fixture = { path: string; name: string; isDir: boolean; size: number; modTime: string; readOnly?: boolean; etag: string; text: string };
    const base = "C:\\work\\fixture";
    const fixtures = new Map<string, Fixture>();
    let revision = 1;
    const make = (path: string, text = "", isDir = false, readOnly = false): Fixture => ({ path, name: path.split("\\").at(-1)!, text, isDir, readOnly, size: text.length, modTime: "2026-10-08T12:00:00Z", etag: `version-${revision++}` });
    const add = (entry: Fixture) => { fixtures.set(entry.path, entry); return entry; };
    add(make(base, "", true));
    add(make(`${base}\\src`, "", true));
    add(make(`${base}\\src\\main.go`, "package main\n\nfunc main() {}\n"));
    add(make(`${base}\\00-readme.ts`, "export const message = 'hello';\n"));
    add(make(`${base}\\01-readonly.json`, '{"protected": true}\n', false, true));
    for (let i = 0; i < 600; i++) add(make(`${base}\\file-${String(i).padStart(4, "0")}.ts`, `export const value = ${i};\n`));
    const roots = [{ id: "root-1", provider: "local", label: "fixture", path: base }];
    const listeners = new Map<string, Set<(value: unknown) => void>>();
    const emit = (name: string, value: unknown) => listeners.get(name)?.forEach((handler) => handler(value));
    const get = (path: unknown) => { const entry = fixtures.get(String(path)); if (!entry) throw new Error("not_found: file does not exist"); return entry; };
    const entryInfo = ({ text: _text, ...entry }: Fixture) => entry;
    const list = (path: unknown) => [...fixtures.values()].filter((item) => item.path.slice(0, item.path.lastIndexOf("\\")) === path).map(entryInfo);
    const changed = (path: string) => emit("files:changed", { provider: "local", dir: path.slice(0, path.lastIndexOf("\\")) });
    const handlers: Record<string, (...args: unknown[]) => unknown> = {
      "Daemon.Health": () => ({ version: "harness", pid: 1, startedAt: "2026-10-08T12:00:00Z", uptimeSec: 1, dbVersion: 1, platform: "windows/amd64", memAllocMB: 1 }),
      "Settings.All": () => ({}), "Workspaces.List": () => [], "Projects.List": () => [], "Worktrees.List": () => [],
      "Agents.List": () => [], "Agents.Runs": () => [], "Agents.Detect": () => [],
      "Policy.Pending": () => [],
      "Plugins.List": () => [], "Desktop.Ready": () => ({ view: "", sequence: 0 }), "Settings.Set": () => null,
      "Updates.Check": () => ({ enabled: false, available: false, error: "" }), "Terminals.List": () => [],
      "Machines.List": () => [{ id: "local", name: "This computer", kind: "local", status: "online" }], "MCP.List": () => [],
      "Agents.Options": () => ({ machine: "local", harnesses: [], projects: [], directories: [], worktree: false, unavailableReason: "" }),
      "Workspaces.Save": (workspace) => ({ ...(workspace as object), id: "files-workspace" }),
      "Notifications.List": () => ({ items: [], sources: [], unread: 0 }), "Notifications.Prefs": () => ({ default: { enabled: true, native: false }, sources: {} }),
      "GitHub.Status": () => ({ connected: false }),
      "Files.Providers": () => [{ id: "local", label: "Local", connected: true, managedRoots: true }, { id: "git", label: "Projects", connected: true, managedRoots: false }, { id: "remote", label: "Remote", connected: false, managedRoots: false, note: "Remote machines are not connected." }, { id: "drive", label: "Drive", connected: false, managedRoots: false, note: "Orole Drive is not connected." }],
      "Files.Roots": (provider) => provider === "git" ? [{ ...roots[0], provider: "git" }] : roots,
      "Files.AddRoot": () => { const root = { id: "root-added", provider: "local", label: "picked-folder", path: "C:\\picked-folder" }; add(make(root.path, "", true)); roots.push(root); return root; },
      "Files.RemoveRoot": (id) => { const index = roots.findIndex((root) => root.id === id); if (index >= 0) roots.splice(index, 1); },
      "Files.List": (_provider, path) => list(path),
      "Files.Stat": (_provider, path) => entryInfo(get(path)),
      "Files.Read": (_provider, path) => { const entry = get(path); return { data: btoa(entry.text), offset: 0, size: entry.size, truncated: false, info: entryInfo(entry) }; },
      "Files.Write": (_provider, path, data, ifMatch, create, force) => {
        const old = fixtures.get(String(path));
        if (old?.readOnly) throw new Error("read_only: file is protected");
        if (create && old) throw new Error("exists: file already exists");
        if (!create && !force && (!old || old.etag !== ifMatch)) return { conflict: true, deleted: !old, current: old && entryInfo(old) };
        const item = add(make(String(path), atob(String(data)))); changed(item.path); return { conflict: false, info: entryInfo(item) };
      },
      "Files.Mkdir": (_provider, path) => { if (fixtures.has(String(path))) throw new Error("exists"); const item = add(make(String(path), "", true)); changed(item.path); return entryInfo(item); },
      "Files.Rename": (_provider, from, to) => { if (fixtures.has(String(to))) throw new Error("exists"); const item = get(from); fixtures.delete(item.path); const next = add({ ...item, path: String(to), name: String(to).split("\\").at(-1)! }); changed(next.path); return entryInfo(next); },
      "Files.Delete": (_provider, path, permanent) => { for (const key of fixtures.keys()) if (key === path || key.startsWith(`${path}\\`)) fixtures.delete(key); changed(String(path)); return { recycled: !permanent }; },
      "Search.Query": (query) => ({ results: [...fixtures.values()].filter((item) => item.name.includes(String(query))).slice(0, 40).map((item) => ({ provider: "files:local", id: item.path, title: item.name, subtitle: base, kind: item.isDir ? "folder" : "file", score: 100, action: { provider: "local", path: item.path, isDir: String(item.isDir), view: "files" } })) }),
    };
    const global = globalThis as unknown as Record<string, unknown>;
    global.mygo = {
      call: async (method: string, ...args: unknown[]) => { const handler = handlers[method]; if (!handler) throw new Error(`Files harness does not provide ${method}`); return structuredClone(handler(...args)); },
      on: (name: string, handler: (value: unknown) => void) => { const group = listeners.get(name) ?? new Set(); group.add(handler); listeners.set(name, group); return () => group.delete(handler); },
      once: () => () => {}, channel: () => ({}), window: {},
    };
    global.__filesHarness = {
      externalChange: () => { const item = get(`${base}\\00-readme.ts`); item.text = "export const message = 'changed externally';\n"; item.etag = `version-${revision++}`; item.size = item.text.length; },
      savedText: () => get(`${base}\\00-readme.ts`).text,
    };
  });
}

async function openFiles(page: Page) {
  await page.getByRole("button", { name: "More", exact: true }).click();
  await page.getByRole("menuitem", { name: "Files", exact: true }).click();
  await expect(page.getByTestId("files-view")).toBeVisible();
}

test("files listing, editor, conflict diff, overwrite and read-only screenshots", async ({ page }, testInfo) => {
  await installFilesBridge(page); await page.goto("/"); await openFiles(page);
  await expect(page.getByText("603 items", { exact: true })).toBeVisible();
  expect(await page.getByTestId("file-list").locator("[data-index]").count()).toBeLessThan(100);
  await page.getByRole("button", { name: "00-readme.ts", exact: true }).click();
  const editor = page.getByLabel("Editor for C:\\work\\fixture\\00-readme.ts");
  await expect(editor).toContainText("export const message");
  await editor.fill("export const message = 'my changes';\n");
  await expect(page.getByText("Unsaved", { exact: true })).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("files-editor.png") });
  await page.evaluate(() => {
    // This shape is installed in this test's init script, never by production code.
    const harness = globalThis as unknown as HarnessGlobal;
    harness.__filesHarness.externalChange();
  });
  await editor.press("Control+s");
  const dialog = page.getByRole("dialog", { name: "File changed on disk" });
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Compare changes" }).click();
  await expect(page.getByTestId("file-conflict-diff")).toContainText("changed externally");
  await page.screenshot({ path: testInfo.outputPath("files-conflict.png") });
  await dialog.getByRole("button", { name: "Overwrite disk version" }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByText("Saved", { exact: true })).toBeVisible();
  expect(await page.evaluate(() => {
    // The test owns both ends of this in-process harness control.
    const harness = globalThis as unknown as HarnessGlobal;
    return harness.__filesHarness.savedText();
  })).toContain("my changes");
  await editor.press("Control+f");
  await expect(page.locator(".cm-search")).toBeVisible();
  await editor.press("Escape");
  await page.getByTestId("file-list").getByRole("button", { name: /^01-readonly\.json/ }).click();
  await expect(page.getByText("This file is read-only.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "Remote files", exact: true }).click();
  await expect(page.getByText("Not connected", { exact: true })).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("files-remote.png") });
});

test("create, rename, delete confirmation, folder tree and native root picker", async ({ page }, testInfo) => {
  await installFilesBridge(page); await page.goto("/"); await openFiles(page);
  await page.getByRole("treeitem", { name: "fixture", exact: true }).click();
  await page.getByRole("treeitem", { name: "src", exact: true }).click();
  await expect(page.getByRole("button", { name: "main.go", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "New file", exact: true }).click();
  await page.getByRole("dialog").getByLabel("Name", { exact: true }).fill("new.ts");
  await page.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByLabel("Editor for C:\\work\\fixture\\src\\new.ts")).toBeVisible();
  await page.getByRole("button", { name: "Rename new.ts", exact: true }).click();
  await page.getByRole("dialog").getByLabel("Name", { exact: true }).fill("renamed.ts");
  await page.getByRole("button", { name: "Rename", exact: true }).click();
  await page.getByRole("button", { name: "Delete renamed.ts", exact: true }).click();
  await expect(page.getByRole("alertdialog")).toContainText("Recycle Bin");
  await page.screenshot({ path: testInfo.outputPath("files-delete.png") });
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(page.getByRole("button", { name: "Delete renamed.ts", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Delete renamed.ts", exact: true }).click();
  await page.getByRole("button", { name: "Move to Recycle Bin", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "File deleted on disk" })).toBeVisible();
  await page.getByRole("button", { name: "Keep editing", exact: true }).click();
  await page.getByRole("button", { name: "Folder", exact: true }).click();
  await page.getByRole("dialog").getByLabel("Name", { exact: true }).fill("new-folder");
  await page.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("button", { name: "new-folder", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Add folder…", exact: true }).click();
  await expect(page.getByRole("treeitem", { name: "picked-folder", exact: true })).toBeVisible();
});

test("palette finds files before the Files view has loaded", async ({ page }, testInfo) => {
  await installFilesBridge(page); await page.goto("/");
  await page.getByRole("button", { name: "Search (Ctrl+K)", exact: true }).click();
  const palette = page.getByRole("dialog", { name: "Command palette" });
  await palette.getByRole("button", { name: "Files", exact: true }).click();
  await palette.getByRole("combobox").fill("00-readme");
  await page.screenshot({ path: testInfo.outputPath("files-palette.png") });
  await palette.getByRole("option", { name: /^00-readme\.ts\b/ }).click();
  await expect(page.getByLabel("Editor for C:\\work\\fixture\\00-readme.ts")).toBeVisible();
});

test("plain browser Files view does not invent data", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "More", exact: true }).click();
  await page.getByRole("menuitem", { name: "Files", exact: true }).click();
  await expect(page.getByText("Files needs the desktop app")).toBeVisible();
});
