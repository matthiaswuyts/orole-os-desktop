import { describe, expect, it } from "vitest";
import { humanizeToolName, toolKind, toolLabel, unwrapShellCommand } from "./toolLabels";
import type { ToolCall } from "./taskEvents";

const call = (patch: Partial<ToolCall>): ToolCall => ({ id: "t", linked: true, name: "tool", server: "", status: "ok", input: undefined, output: "", summary: "", ...patch });

describe("unwrapShellCommand", () => {
  it("strips the PowerShell wrapper Codex reports on Windows", () => {
    expect(unwrapShellCommand(`"C:\\Program Files\\PowerShell\\7\\pwsh.exe" -Command 'Get-ChildItem -Force'`)).toBe("Get-ChildItem -Force");
    expect(unwrapShellCommand(`powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -Command "git status"`)).toBe("git status");
    expect(unwrapShellCommand(`pwsh -c 'ls'`)).toBe("ls");
  });

  it("strips cmd and POSIX shell wrappers, and leaves ordinary commands alone", () => {
    expect(unwrapShellCommand("cmd /c dir /b")).toBe("dir /b");
    expect(unwrapShellCommand(`bash -lc "npm test -- --run"`)).toBe("npm test -- --run");
    expect(unwrapShellCommand("/bin/sh -c 'echo hi'")).toBe("echo hi");
    expect(unwrapShellCommand("npm run build")).toBe("npm run build");
    expect(unwrapShellCommand(`pwsh -File build.ps1`)).toBe("pwsh -File build.ps1");
  });

  it("unwraps nested wrappers but gives up on a malformed one", () => {
    expect(unwrapShellCommand(`bash -lc 'pwsh -Command "echo deep"'`)).toBe("echo deep");
    expect(unwrapShellCommand("bash")).toBe("bash");
  });
});

describe("toolLabel", () => {
  it("shows the real command for a shell call instead of the wrapper or raw JSON", () => {
    const label = toolLabel(call({ name: "shell", input: { command: `"C:\\Program Files\\PowerShell\\7\\pwsh.exe" -Command 'npm test'`, cwd: "C:\\work" } }));
    expect(label).toMatchObject({ kind: "shell", verb: "Ran", chip: "npm test" });
    expect(toolLabel(call({ name: "Bash", status: "running", input: { command: "npm test" } })).verb).toBe("Running");
  });

  it("uses only the first line of a multi-line command and truncates long ones", () => {
    expect(toolLabel(call({ name: "bash", input: { command: "echo one\necho two" } })).chip).toBe("echo one");
    const long = toolLabel(call({ name: "bash", input: { command: "x".repeat(300) } }));
    expect(long.chip.length).toBeLessThanOrEqual(100);
    expect(long.title).toBe("x".repeat(300));
  });

  it("reads legacy text-only calls", () => {
    expect(toolLabel(call({ name: "Bash", input: undefined, summary: "npm test" })).chip).toBe("npm test");
  });

  it("omits the chip for a web search without a query, never printing the raw arguments", () => {
    const empty = toolLabel(call({ name: "web_search", input: { action: null, query: "" }, summary: '{"action":null,"query":""}' }));
    expect(empty).toMatchObject({ kind: "web-search", verb: "Searched the web", chip: "" });
    expect(toolLabel(call({ name: "web_search", input: { query: "assistant-ui elements" } }))).toMatchObject({ verb: "Searched the web", chip: "assistant-ui elements" });
    expect(toolLabel(call({ name: "web_search", input: { action: { type: "search", query: "nested query" } } })).chip).toBe("nested query");
  });

  it("names reads by file basename", () => {
    expect(toolLabel(call({ name: "Read", input: { file_path: "C:\\code\\orole\\src\\main.go" } }))).toMatchObject({ kind: "read", verb: "Read", chip: "main.go" });
    expect(toolLabel(call({ name: "view_image", input: { path: "/tmp/a.png" } })).verb).toBe("Viewed");
  });

  it("summarizes a patch by file with +/- counts from the call data", () => {
    const patch = toolLabel(call({ name: "apply_patch", input: { changes: [
      { path: "src/a.ts", kind: "update", diff: "@@\n-old\n+new\n+newer\n" },
      { path: "src/b.ts", kind: "add" },
    ] } }));
    expect(patch).toMatchObject({ kind: "edit", verb: "Edited", chip: "a.ts, b.ts" });
    expect(patch.stats).toEqual([{ file: "a.ts", added: 2, removed: 1 }]);
    const edit = toolLabel(call({ name: "Edit", input: { file_path: "src/c.ts", old_string: "a\nb", new_string: "a\nb\nc" } }));
    expect(edit).toMatchObject({ chip: "c.ts", stats: [{ file: "c.ts", added: 3, removed: 2 }] });
    const text = toolLabel(call({ name: "apply_patch", input: { input: "*** Begin Patch\n*** Update File: x/y.go\n-a\n+b\n*** End Patch" } }));
    expect(text.stats).toEqual([{ file: "y.go", added: 1, removed: 1 }]);
    expect(toolLabel(call({ name: "Write", status: "running", input: { file_path: "n.txt" } })).verb).toBe("Editing");
  });

  it("groups grep and glob as a file search", () => {
    expect(toolLabel(call({ name: "Grep", input: { pattern: "TODO", path: "src" } }))).toMatchObject({ kind: "files", verb: "Searched files", chip: "TODO" });
    expect(toolLabel(call({ name: "glob", input: { pattern: "**/*.ts" } })).verb).toBe("Searched files");
  });

  it("labels MCP tools by server and tool, however the server arrives", () => {
    expect(toolLabel(call({ name: "skill_list", server: "orole", input: {} }))).toMatchObject({ kind: "mcp", verb: "Used", chip: "orole · Skill list" });
    expect(toolLabel(call({ name: "mcp__docs__search_pages", input: { q: 1 } }))).toMatchObject({ kind: "mcp", chip: "docs · Search pages" });
    expect(toolKind("mcp__a__b")).toBe("mcp");
  });

  it("humanizes unknown tools and still gives them a useful chip", () => {
    expect(humanizeToolName("agent_spawnHelper")).toBe("Agent spawn helper");
    expect(toolLabel(call({ name: "deploy_site", input: { description: "Ship it" } }))).toMatchObject({ kind: "other", verb: "Deploy site", chip: "Ship it" });
    expect(toolLabel(call({ name: "deploy_site" })).chip).toBe("");
  });
});
