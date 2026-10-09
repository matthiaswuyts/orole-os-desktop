import { expect, test, type Page } from "@playwright/test";
import type { LaunchOptions, Project, Session, StartSpec } from "../src/mygo";

interface Recorded { method: string; args: unknown[] }

/** mygo-runtime calls globalThis.mygo directly. Fixtures and call recording exist only in this harness. */
async function installBridge(page: Page) {
  await page.addInitScript(() => {
    const now = Date.now();
    const project = (id: string, name: string, remoteUrl: string): Project => ({ id, name, machineId: "local", path: `C:\\code\\${name}`, remoteUrl, lastOpened: 1, createdAt: 0, worktreeRoot: "", worktreeRootCustom: false, missing: false, isGit: true });
    const projects = [project("p-shop", "checkout-fix", "https://github.com/me/checkout-fix.git"), project("p-notes", "notes", "")];
    const harness = (id: string, name: string) => ({ id, name, available: true, executable: id, version: "1", reasoning: [], customModel: true, modes: ["structured"], structured: true, interactive: false, unavailableReason: "", models: [{ id: `${id}-default`, name: `${name} default`, reasoning: [], default: true }] });
    const options: LaunchOptions = { machine: "local", projects: [], directories: [], worktree: true, unavailableReason: "", harnesses: [harness("claude", "Claude Code"), harness("codex", "Codex")] };
    const session = (id: string, title: string, over: Partial<Session> = {}): Session => ({ id, title, projectId: "p-shop", project: "C:\\code\\checkout-fix", dir: "C:\\code\\checkout-fix", machine: "local", kind: "claude", status: "needs-you", review: "", updatedAt: new Date(now - 7 * 60_000).toISOString(), ...over }) as Session;
    const sessions = [session("s-old", "Fix checkout webhook retries")];
    const calls: Recorded[] = [];
    const g = globalThis as unknown as { __calls: Recorded[]; mygo: unknown };
    g.__calls = calls;
    const handlers: Record<string, (...args: never[]) => unknown> = {
      "Daemon.Health": () => ({ version: "0.1.0", memAllocMB: 24, startedAt: new Date(now).toISOString() }), "Settings.All": () => ({}), "Workspaces.List": () => [], "Workspaces.Save": (w: object) => ({ ...w, id: "local" }),
      "Projects.List": () => projects, "Worktrees.List": () => [], "Notifications.List": () => ({ items: [], sources: [], unread: 0 }),
      "Notifications.Prefs": () => ({ default: { enabled: true, native: false, sound: false }, sources: {} }), "Policy.Pending": () => [], "Plugins.List": () => [], "Desktop.Ready": () => ({ view: "", sequence: 0 }),
      "Updates.Check": () => ({ enabled: false, available: false, error: "" }), "Terminals.List": () => [], "Machines.List": () => [{ id: "local", name: "devl", kind: "local", status: "online" }], "MCP.List": () => [],
      "Settings.Set": () => null, "Agents.List": () => sessions, "Agents.Runs": () => [], "Agents.Options": () => options,
      "Agents.Start": (spec: StartSpec) => { calls.push({ method: "Agents.Start", args: [spec] }); return session("s-new", "New task", { status: "working" }); },
    };
    g.mygo = {
      call: async (method: string, ...args: unknown[]) => {
        const fn = handlers[method] as ((...a: unknown[]) => unknown) | undefined;
        if (!fn) throw new Error(`Harness method not provided: ${method}`);
        return structuredClone(fn(...args));
      },
      on: () => () => {}, once: () => () => {}, channel: () => ({}), window: {},
    };
  });
}

test("New task on a project is a page with ascii art, the Home input and quick actions", async ({ page }, testInfo) => {
  await installBridge(page);
  await page.goto("/");
  await page.getByRole("button", { name: "New task in checkout-fix" }).click();

  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.getByRole("heading", { name: "checkout-fix", level: 1 })).toBeVisible();
  await expect(page.getByText("me/checkout-fix")).toBeVisible();
  const art = page.getByTestId("ascii-mark");
  await expect(art).toHaveAttribute("data-piece", /^(donut|gyroscope|icosahedron|black-hole|planet|rocket|harmonograph|campfire|snake|whale|lighthouse|coffee|windmill)$/);
  await expect(async () => expect((await art.textContent())!.trim().length).toBeGreaterThan(20)).toPass();
  await expect(page.getByPlaceholder("What should an agent do in checkout-fix?")).toBeFocused();
  const recent = page.getByRole("region", { name: "Pick up where you left off" });
  await expect(recent.getByText("Fix checkout webhook retries")).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("new-task-page.png") });

  await page.getByRole("region", { name: "Quick actions" }).getByRole("button", { name: /^New Codex/ }).click();
  await page.getByLabel("Task for your agent").fill("Fix the retry backoff");
  await page.getByRole("button", { name: "Start task" }).click();
  const [[spec]] = await page.evaluate(() => (globalThis as unknown as { __calls: Recorded[] }).__calls.filter((c) => c.method === "Agents.Start").map((c) => c.args)) as [[StartSpec]];
  expect(spec).toMatchObject({ projectId: "p-shop", kind: "codex", prompt: "Fix the retry backoff" });
});
