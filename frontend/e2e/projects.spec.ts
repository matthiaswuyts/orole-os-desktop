import { expect, test, type Page } from "@playwright/test";

/**
 * The MyGo bridge only exists inside the desktop window, so this spec installs a `window.mygo` shim
 * for the browser preview. The shim lives in the test only; product code never fakes data.
 */
async function installBridge(page: Page) {
  await page.addInitScript(() => {
    type W = Record<string, unknown>;
    const mk = (id: string, name: string, over: W = {}) => ({ id, name, machineId: "local", path: `C:\\code\\${name}`, remoteUrl: "", lastOpened: Math.floor(Date.now() / 1000), createdAt: 0, worktreeRoot: `C:\\code\\.orole-worktrees\\${name}`, worktreeRootCustom: false, missing: false, ...over });
    const wt = (id: string, projectId: string, name: string, over: W = {}) => ({ id, projectId, name, path: `C:\\code\\.orole-worktrees\\x\\${name}`, branch: `orole/${name}`, head: "abcdef1234", baseRef: "main", isMain: false, detached: false, locked: false, prunable: false, createdAt: 0, upstream: "", ahead: 0, behind: 0, staged: 0, unstaged: 0, untracked: 0, dirty: 0, ...over });
    const state: { projects: ReturnType<typeof mk>[]; wts: Record<string, ReturnType<typeof wt>[]> } = {
      projects: [mk("p1", "orole-os", { remoteUrl: "https://github.com/rayorole/orole-os-desktop.git" })],
      wts: { p1: [wt("w0", "p1", "orole-os", { isMain: true, branch: "main", upstream: "origin/main", dirty: 3, unstaged: 2, untracked: 1 }), ...["alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta"].map((n, i) => wt(`w${i + 1}`, "p1", n, i === 1 ? { dirty: 1, unstaged: 1 } : i === 2 ? { ahead: 2, upstream: "origin/x" } : {}))] },
    };
    const lines: unknown[] = [];
    for (let i = 1; i <= 6000; i++) lines.push({ kind: i % 7 === 0 ? "+" : i % 11 === 0 ? "-" : " ", text: `line ${i} of a very large file`, old: i, new: i });
    const bigDiff = { files: [{ path: "src/big.ts", status: "modified", binary: false, additions: 850, deletions: 540, hunks: [{ header: "@@ -1,6000 +1,6000 @@", oldStart: 1, oldLines: 6000, newStart: 1, newLines: 6000, lines }] }, { path: "logo.png", status: "added", binary: true, additions: 0, deletions: 0, hunks: [] }], additions: 850, deletions: 540, truncated: false };
    const handlers: Record<string, (...a: any[]) => unknown> = {
      "Daemon.Health": () => ({ version: "test", pid: 1, startedAt: new Date().toISOString(), uptimeSec: 1, dbVersion: 10, platform: "windows/amd64", goVersion: "go", goroutines: 1, memAllocMB: 1, terminals: 0, protocolVersion: 1 }),
      "Settings.All": () => ({}),
      "Workspaces.List": () => [],
      "Workspaces.Save": (w: W) => ({ ...w, id: "ws1" }),
      "Settings.Set": () => null, "Policy.Pending": () => [], "Plugins.List": () => [],
      "Desktop.Ready": () => ({ view: "", sequence: 0 }), "Updates.Check": () => ({ enabled: false, available: false, error: "" }),
      "Agents.List": () => [], "Agents.Runs": () => [], "Agents.Detect": () => [], "Terminals.List": () => [],
      "Machines.List": () => [{ id: "local", name: "This computer", kind: "local", status: "online" }], "MCP.List": () => [],
      "Agents.Options": () => ({ machine: "local", harnesses: [], projects: [], directories: [], worktree: false, unavailableReason: "" }),
      "Notifications.List": () => ({ items: [], sources: [], unread: 0 }),
      "Notifications.Prefs": () => ({ default: { enabled: true, native: false, sound: false }, sources: {} }),
      "Projects.List": () => state.projects,
      "Projects.PickFolder": () => "C:\\code\\newrepo",
      "Projects.Add": (path: string) => { const p = mk("p2", path.split("\\").pop()!); state.projects.push(p); state.wts.p2 = [wt("m2", "p2", p.name, { isMain: true, branch: "main" })]; return p; },
      "Projects.Open": () => null,
      "Worktrees.List": (id: string) => state.wts[id] ?? [],
      "Worktrees.Create": (id: string, name: string) => { const w = wt(`n${Date.now()}`, id, name.toLowerCase().replace(/\W+/g, "-")); state.wts[id]!.push(w); return w; },
      "Worktrees.Remove": (id: string) => { for (const k of Object.keys(state.wts)) state.wts[k] = state.wts[k]!.filter((w) => w.id !== id); return null; },
      "Git.Status": () => ({ branch: { oid: "abc", head: "main", upstream: "origin/main", ahead: 1, behind: 0 }, files: [{ path: "src/big.ts", index: ".", worktree: "M", kind: "modified" }], staged: 0, unstaged: 1, untracked: 0, conflicted: 0, dirty: 1 }),
      "Git.Branches": () => [{ name: "main", remote: false, current: true, hash: "abc1234", upstream: "origin/main", ahead: 1, behind: 0, gone: false, date: 0, subject: "feat: sidebar" }],
      "Git.Log": () => [{ hash: "a".repeat(40), short: "aaaaaaa", author: "ray", email: "r@x", date: Math.floor(Date.now() / 1000) - 600, refs: ["HEAD -> main"], subject: "feat: sidebar" }],
      "Git.Diff": () => bigDiff,
    };
    (globalThis as unknown as W).mygo = {
      call: async (method: string, ...args: unknown[]) => { const h = handlers[method]; if (!h) throw new Error(`shim: ${method} not stubbed`); return h(...args); },
      on: () => () => {}, once: () => () => {}, channel: () => ({}), window: {},
    };
  });
}

test("sidebar tree shows the project, worktrees and the 'more worktrees' collapse", async ({ page }, testInfo) => {
  await installBridge(page);
  await page.goto("/");
  const tree = page.getByRole("region", { name: "Projects" });
  await expect(tree.getByText("orole-os", { exact: true }).first()).toBeVisible();
  await expect(tree.getByText("local", { exact: true })).toBeVisible();
  await expect(tree.getByText("main checkout")).toBeVisible();
  await expect(tree.getByText("3 more worktrees")).toBeVisible();
  await expect(tree.getByRole("img", { name: "uncommitted changes" })).toHaveCount(2);
  await page.screenshot({ path: testInfo.outputPath("sidebar.png") });
  await tree.getByText("3 more worktrees").click();
  await expect(tree.getByText("eta", { exact: true })).toBeVisible();
  await tree.getByRole("button", { name: "Collapse orole-os" }).click();
  await expect(tree.getByText("main checkout")).toBeHidden();
});

test("worktrees view lists, creates and removes with confirmation", async ({ page }, testInfo) => {
  await installBridge(page);
  await page.goto("/");
  await page.getByRole("button", { name: "Worktrees", exact: true }).click();
  await expect(page.getByText("8 worktrees")).toBeVisible();
  await expect(page.getByRole("row")).toHaveCount(9); // header + 8
  await page.getByRole("button", { name: "New worktree" }).click();
  await page.getByRole("menuitem", { name: "in orole-os" }).click();
  await page.getByLabel("Name").fill("Fix Login!");
  await expect(page.getByText("orole/fix-login")).toBeVisible();
  await page.getByRole("button", { name: "Create worktree" }).click();
  await expect(page.getByRole("row")).toHaveCount(10);
  await page.getByRole("button", { name: "Remove worktree beta" }).click();
  await expect(page.getByRole("alertdialog")).toContainText("Remove worktree beta?");
  await expect(page.getByRole("alertdialog")).toContainText("uncommitted");
  await page.screenshot({ path: testInfo.outputPath("worktrees.png") });
  await page.getByRole("button", { name: "Remove worktree", exact: true }).click();
  await expect(page.getByRole("row")).toHaveCount(9);
});

test("projects view: detail, and a 6000-line diff stays virtualized", async ({ page }, testInfo) => {
  await installBridge(page);
  await page.goto("/");
  await page.getByRole("region", { name: "Projects" }).getByText("orole-os", { exact: true }).first().click();
  await expect(page.getByRole("heading", { name: "orole-os" })).toBeVisible();
  await expect(page.getByText("Recent commits")).toBeVisible();
  await page.getByRole("button", { name: "View changes vs HEAD" }).click();
  const diff = page.getByTestId("diff-view");
  await expect(diff).toBeVisible();
  await expect(diff.getByText("2 files")).toBeVisible();
  const mounted = await diff.locator("[data-index]").count();
  expect(mounted).toBeGreaterThan(5);
  expect(mounted).toBeLessThan(150);
  await diff.getByRole("button", { name: /logo\.png/ }).click();
  await expect(diff.getByText("Binary file not shown")).toBeVisible();
  await diff.scrollIntoViewIfNeeded();
  await page.screenshot({ path: testInfo.outputPath("diff.png") });
});

test("add via folder picker", async ({ page }) => {
  await installBridge(page);
  await page.goto("/");
  await page.getByRole("button", { name: "Project actions" }).click();
  await page.getByRole("menuitem", { name: /Add existing folder/ }).click();
  await expect(page.getByRole("heading", { name: "newrepo" })).toBeVisible();
});

test("browser preview without a bridge shows the desktop-only state, no fake projects", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("region", { name: "Projects" })).toContainText("Projects need the desktop app");
});
