import { describe, expect, it } from "vitest";
import { describePermission } from "./permissionLabels";

const request = (tool: string, input: unknown, summary = "") => ({ tool, summary, input, inputJSON: JSON.stringify(input) });

describe("describePermission", () => {
  it("shows the exact command and folder for a shell request", () => {
    const command = `"C:\\Program Files\\PowerShell\\7\\pwsh.exe" -Command 'git status'`;
    expect(describePermission(request("shell", { kind: "command", command, cwd: "C:\\work", reason: "needs to inspect the tree" }, "git status — needs to inspect the tree"))).toEqual({
      capability: "Run a command",
      reach: [{ label: "Command", value: command }, { label: "Folder", value: "C:\\work" }, { label: "Why", value: "needs to inspect the tree" }],
    });
    expect(describePermission(request("Bash", { command: "rm x" })).reach).toEqual([{ label: "Command", value: "rm x" }]);
    expect(describePermission(request("shell", { command: ["git", "commit", "-m", "a b"], cwd: "." })).reach[0]).toEqual({ label: "Command", value: 'git commit -m "a b"' });
  });

  it("names network access separately from the command that needs it", () => {
    const description = describePermission(request("shell", { command: "curl x", networkApprovalContext: { host: "example.com", protocol: "https" } }));
    expect(description.capability).toBe("Access the network");
    expect(description.reach[0]).toEqual({ label: "Network", value: "example.com (https)" });
  });

  it("lists the files of a change", () => {
    expect(describePermission(request("apply_patch", { changes: [{ path: "a.go", kind: "update" }, { path: "b.go", kind: "add" }, { path: "c.go", kind: "update", movePath: "d.go" }] }))).toEqual({
      capability: "Edit files", reach: [{ label: "Files", value: "a.go\nadd b.go\nc.go → d.go" }],
    });
    expect(describePermission(request("Write", { file_path: "src/x.ts", content: "..." })).reach).toEqual([{ label: "Files", value: "src/x.ts" }]);
    expect(describePermission(request("apply_patch", { fileChanges: { "legacy.txt": {} } })).reach[0]!.value).toBe("legacy.txt");
  });

  it("describes MCP servers and extra-permission grants", () => {
    expect(describePermission(request("mcp:docs", { message: "Allow docs to run search?" }, "Allow docs to run search?"))).toEqual({ capability: "Use docs", reach: [{ label: "Request", value: "Allow docs to run search?" }] });
    expect(describePermission(request("mcp__orole__skill_list", {}, "skill_list")).capability).toBe("Use orole");
    const grant = describePermission(request("permissions", { cwd: "C:\\w", permissions: { fileSystem: { read: ["C:\\a"], write: ["C:\\b"] }, network: { enabled: true } } }));
    expect(grant.capability).toBe("Extra access for this turn");
    expect(grant.reach).toEqual([{ label: "Read", value: "C:\\a" }, { label: "Write", value: "C:\\b" }, { label: "Network", value: "Allow network access" }, { label: "Folder", value: "C:\\w" }]);
  });

  it("falls back to a one-line summary for unknown tools and tolerates unusable input", () => {
    expect(describePermission({ tool: "deploy_site", summary: "Deploy to prod", input: undefined, inputJSON: "" })).toEqual({ capability: "Use deploy site", reach: [{ label: "Request", value: "Deploy to prod" }] });
    expect(describePermission({ tool: "shell", summary: "ls", input: undefined, inputJSON: "not json" }).reach).toEqual([{ label: "Command", value: "ls" }]);
  });
});
