import { expect, test, type Page } from "@playwright/test";
import type { Session, Detection } from "../src/mygo";

/** mygo-runtime calls globalThis.mygo directly. Fixtures exist only in this harness. */
async function installAgentsBridge(page: Page) {
  await page.addInitScript(() => {
    const now = Date.now();
    const make = (id: string, title: string, status: Session["status"], overrides: Partial<Session> = {}): Session => ({ id, title, status, kind: "claude", machine: "local", project: "C:\\code\\orole-os", dir: "C:\\code\\orole-os", branch: "feat/agent-dashboard", baseRef: "abc123", worktreeOwned: false, model: "sonnet", task: title, mode: "structured", permissionMode: "default", pid: 123, live: true, activity: "", tail: "", lastMessage: "", agentSessionId: "cli-session", terminalId: "", transcriptRef: "", parentId: "", group: "", review: "", reviewNote: "", checkState: "", checkCommand: "", checkOutput: "", files: 0, additions: 0, deletions: 0, turns: 1, prompts: 1, pending: [], result: "", error: "", startedAt: new Date(now - 720000).toISOString(), updatedAt: new Date(now - 1000).toISOString(), ...overrides });
    const request = { id: "permission-1", tool: "Bash", summary: "bun run test — run the frontend test suite", input: { command: "bun run test" }, inputJSON: '{"command":"bun run test"}', at: new Date(now).toISOString() };
    const sessions = [make("needs", "Verify workspace permissions", "needs-you", { activity: "Waiting for permission", pending: [request], tail: "Ready to run the updated permission tests." }), make("working", "Build the agent dashboard", "working", { kind: "codex", model: "gpt-5-codex", activity: "Updating session cards", tail: "Reading existing component styles…\nApplying the dashboard layout\nConnecting live session events" }), make("done", "Fix project path handling", "done", { kind: "opencode", model: "", branch: "fix/windows-paths", activity: "Turn complete", lastMessage: "Updated path normalization for Windows worktrees.\n\nPaths now preserve drive roots and consistently resolve trailing separators. Added regression coverage for mixed separators and UNC paths.\n\nAll 18 path tests pass.", files: 2, additions: 24, deletions: 6, checkState: "passing", checkCommand: "bun run test", checkOutput: "18 tests passed", tail: "All 18 path tests pass.", updatedAt: new Date(now - 60000).toISOString() }), make("lost", "Document daemon recovery", "lost", { kind: "gemini", model: "gemini", live: false, branch: "docs/recovery", activity: "Process lost after daemon exit", tail: "", updatedAt: new Date(now - 240000).toISOString() }), make("failed", "Check CI configuration", "failed", { kind: "custom", model: "", live: false, error: "Command exited with code 1: missing CI_TOKEN", branch: "fix/ci", updatedAt: new Date(now - 120000).toISOString() })];
    const detections: Detection[] = ["claude", "codex", "opencode", "gemini", "custom"].map((kind) => ({ kind, name: ({ claude: "Claude Code", codex: "Codex", opencode: "OpenCode", gemini: "Gemini CLI", custom: "Custom CLI" } as Record<string, string>)[kind], installed: kind !== "gemini", path: "C:\\tools\\agent.exe", version: kind === "claude" ? "2.1.4" : "1.0.0", error: kind === "gemini" ? "gemini executable not found on PATH" : "", modes: kind === "claude" ? ["structured", "terminal"] : ["terminal"], resume: kind === "claude", permissions: kind === "claude", statusFeed: kind === "claude" ? "stream-json" : "process" }));
    const listeners = new Map<string, Set<(v: unknown) => void>>();
    const emit = (name: string, data: unknown) => listeners.get(name)?.forEach((fn) => fn(data));
    const get = (id: unknown) => { const s = sessions.find((x) => x.id === id); if (!s) throw new Error("Session not found"); return s; };
    const diff = { files: [{ path: "src/lib/paths.ts", status: "modified", binary: false, additions: 2, deletions: 1, hunks: [{ header: "@@ -1,3 +1,4 @@", oldStart: 1, oldLines: 3, newStart: 1, newLines: 4, lines: [{ kind: " ", text: "export function normalizePath(path: string) {", old: 1, new: 1 }, { kind: "-", text: "  return path.replace(/\\\\/g, '/');", old: 2, new: 0 }, { kind: "+", text: "  const normalized = path.replace(/\\\\/g, '/');", old: 0, new: 2 }, { kind: "+", text: "  return preserveDriveRoot(normalized);", old: 0, new: 3 }, { kind: " ", text: "}", old: 3, new: 4 }] }] }], additions: 2, deletions: 1, truncated: false };
    const handlers: Record<string, (...args: unknown[]) => unknown> = {
      "Daemon.Health": () => ({ version: "0.1.0", memAllocMB: 24, startedAt: new Date(now).toISOString() }), "Settings.All": () => ({}),
      "Projects.List": () => [{ id: "p1", name: "orole-os", path: "C:\\code\\orole-os", machineId: "local", missing: false }], "Worktrees.List": () => [],
      "Notifications.List": () => ({ items: [], unread: 0, needsYou: 0 }), "Notifications.Prefs": () => ({ kinds: {} }),
      "Policy.Pending": () => [], "Plugins.List": () => [], "Desktop.Ready": () => ({ view: "", sequence: 0 }),
      "Updates.Check": () => ({ enabled: false, available: false, error: "" }), "Terminals.List": () => [],
      "Machines.List": () => [{ id: "local", name: "This computer", kind: "local", status: "online" }],
      "MCP.List": () => [], "Settings.Set": () => null,
      "Agents.Options": () => ({ machine: "local", harnesses: [], projects: [], directories: [], worktree: false, unavailableReason: "" }),
      "Agents.List": () => sessions, "Agents.Runs": () => [{ id: "run1", kind: "loop-until", status: "running", key: "fixture", params: {}, state: {}, error: "", createdAt: new Date(now).toISOString(), updatedAt: new Date(now).toISOString() }], "Agents.Detect": () => detections, "Agents.Get": get,
      "Agents.Transcript": (id) => [{ id: 1, sessionId: id, at: new Date(now).toISOString(), kind: "prompt", text: "Verify workspace permissions and run the frontend tests.", data: null }, { id: 2, sessionId: id, at: new Date(now).toISOString(), kind: "message", text: "I reviewed the permission flow and updated the regression coverage. I’d like to run the test suite before proceeding.", data: null }, { id: 3, sessionId: id, at: new Date(now).toISOString(), kind: "tool", text: "Read frontend/src/stores/permissions.ts", data: null }, { id: 4, sessionId: id, at: new Date(now).toISOString(), kind: "permission", text: "Bash", data: request }],
      "Agents.Diff": (id) => ({ sessionId: id, dir: "C:\\code\\orole-os", baseRef: "abc123", files: 1, additions: 2, deletions: 1, truncated: false, error: "", diff }),
      "Agents.Approve": (id) => { const s = get(id); s.pending = []; s.status = "working"; s.updatedAt = new Date().toISOString(); emit("agents:updated", s); },
      "Agents.Prompt": (id, text) => { const s = get(id); s.status = "working"; s.updatedAt = new Date().toISOString(); emit("agents:updated", s); emit("agents:event", { id: 5, sessionId: id, kind: "prompt", at: new Date().toISOString(), text, data: null }); return s; },
      "Agents.LoopUntil": () => { throw new Error("Codex does not report when a turn ends, so loop-until cannot tell when to re-check"); },
      "Agents.Review": (id, decision) => { const s = get(id); s.review = decision === "approve" ? "approved" : decision === "discard" ? "discarded" : "sent-back"; s.updatedAt = new Date().toISOString(); emit("agents:updated", s); return s; },
    };
    (globalThis as unknown as Record<string, unknown>).mygo = { call: async (method: string, ...args: unknown[]) => { const fn = handlers[method]; if (!fn) throw new Error(`Harness method not provided: ${method}`); return structuredClone(fn(...args)); }, on: (name: string, fn: (data: unknown) => void) => { const set = listeners.get(name) ?? new Set(); set.add(fn); listeners.set(name, set); return () => set.delete(fn); }, once: () => () => {}, channel: () => ({}), window: {} };
  });
}

test("dashboard, detail, permission approval and review with a test-only MyGo bridge", async ({ page }, testInfo) => {
  await installAgentsBridge(page);
  await page.goto("/");
  await page.getByRole("button", { name: /^Agent Dashboard/ }).click();
  await expect(page.getByRole("region", { name: "Needs you", exact: true }).getByRole("article")).toHaveCount(2);
  await expect(page.getByRole("region", { name: "Working", exact: true }).getByRole("article")).toHaveCount(1);
  await expect(page.getByRole("region", { name: "Done", exact: true }).getByRole("article")).toHaveCount(1);
  await expect(page.getByRole("region", { name: "Idle", exact: true }).getByRole("article")).toHaveCount(1);
  await page.screenshot({ path: testInfo.outputPath("agents-dashboard.png") });
  await page.getByRole("article", { name: "Verify workspace permissions" }).getByRole("button", { name: "Open", exact: true }).click();
  await expect(page.getByText("Read frontend/src/stores/permissions.ts")).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("agents-detail.png") });
  await expect(page.getByRole("button", { name: "Allow", exact: true })).toBeDisabled();
  await page.getByRole("checkbox", { name: "I reviewed the complete tool name and arguments." }).check();
  await page.getByRole("button", { name: "Allow", exact: true }).click();
  await expect(page.getByText("Resolved", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Back to tasks" }).click();
  await page.getByRole("button", { name: "New agent", exact: true }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("agents-start-dialog.png") });
  await page.getByRole("dialog").getByRole("button", { name: "Close", exact: true }).click();
  await page.getByRole("button", { name: /^Review\s/ }).click();
  await expect(page.getByTestId("diff-view")).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("review.png") });
  await page.getByRole("button", { name: "Approve", exact: true }).click();
  await expect(page.getByText("Approved", { exact: true }).first()).toBeVisible();
});

test("plain browser preview has no fake agent sessions", async ({ page }) => {
  await page.goto("/"); await page.getByRole("button", { name: "Agent Dashboard", exact: true }).click();
  await expect(page.getByText("Agents needs the desktop app")).toBeVisible();
  await expect(page.getByRole("article")).toHaveCount(0);
});
