import type { PermissionRequest } from "@/mygo";
import { humanizeToolName, shellCommand, toolKind } from "./toolLabels";
import { isRecord } from "./taskEvents";

/** One labelled line of what an approval would let happen: a command, a folder, the files. */
export interface PermissionReach { label: string; value: string }
export interface PermissionDescription {
  /** Plain words: "Run a command", "Edit files", "Use docs". */
  capability: string;
  reach: PermissionReach[];
}

const str = (v: unknown): string => typeof v === "string" ? v : "";
const strings = (v: unknown): string[] => Array.isArray(v) ? v.filter((s): s is string => typeof s === "string" && s !== "") : [];

/** The exact snapshot the user is approving, parsed; falls back to the daemon's own parsed copy. */
function inputOf(request: Pick<PermissionRequest, "input" | "inputJSON">): Record<string, unknown> {
  if (request.inputJSON) {
    try {
      const parsed: unknown = JSON.parse(request.inputJSON);
      if (isRecord(parsed)) return parsed;
    } catch { /* fall through to the provider's parsed copy */ }
  }
  return isRecord(request.input) ? request.input : {};
}

const KIND_CAPABILITY = { shell: "Run a command", edit: "Edit files", read: "Read files", files: "Search files", "web-fetch": "Access the network", "web-search": "Search the web", image: "View an image", agent: "Run a sub-agent" } as const;

function filePaths(input: Record<string, unknown>): string[] {
  const out: string[] = [];
  if (Array.isArray(input.changes)) {
    for (const change of input.changes.filter(isRecord)) {
      const path = str(change.path);
      if (!path) continue;
      const kind = str(change.kind), move = str(change.movePath);
      out.push(`${kind && kind !== "update" ? `${kind} ` : ""}${path}${move ? ` → ${move}` : ""}`);
    }
  }
  if (isRecord(input.fileChanges)) out.push(...Object.keys(input.fileChanges));
  if (!out.length) for (const key of ["file_path", "filePath", "path", "notebook_path", "target_file"]) { const path = str(input[key]); if (path) { out.push(path); break; } }
  return out;
}

/** Codex "extra permissions for this turn" requests: read/write paths and network access. */
function grantedAccess(input: Record<string, unknown>): PermissionReach[] {
  const permissions = isRecord(input.permissions) ? input.permissions : {};
  const fileSystem = isRecord(permissions.fileSystem) ? permissions.fileSystem : {};
  const network = isRecord(permissions.network) ? permissions.network : {};
  const reach: PermissionReach[] = [];
  const read = strings(fileSystem.read), write = strings(fileSystem.write);
  if (read.length) reach.push({ label: "Read", value: read.join("\n") });
  if (write.length) reach.push({ label: "Write", value: write.join("\n") });
  if (Array.isArray(fileSystem.entries) && fileSystem.entries.length) reach.push({ label: "File rules", value: `${fileSystem.entries.length} file-system rule(s); see Details` });
  if (typeof network.enabled === "boolean") reach.push({ label: "Network", value: network.enabled ? "Allow network access" : "Block network access" });
  return reach;
}

/**
 * What a permission request means in plain words, from the tool the harness named and the exact input snapshot. Everything
 * returned is untrusted agent text: render it inert. Details such as a diff or file contents are not reproduced; the raw request
 * stays available behind a disclosure.
 */
export function describePermission(request: Pick<PermissionRequest, "tool" | "summary" | "input" | "inputJSON">): PermissionDescription {
  const input = inputOf(request);
  const mcpServer = request.tool.startsWith("mcp:") ? request.tool.slice(4) : "";
  const claudeMcp = /^mcp__([^_].*?)__(.+)$/.exec(request.tool);
  const kind = mcpServer || claudeMcp ? "mcp" : toolKind(request.tool);
  const reach: PermissionReach[] = [];
  const push = (label: string, value: string) => { if (value.trim()) reach.push({ label, value }); };
  let capability: string;

  const network = isRecord(input.networkApprovalContext) ? input.networkApprovalContext : null;
  if (kind === "mcp") {
    capability = `Use ${mcpServer || claudeMcp![1]}`;
    if (claudeMcp) push("Tool", humanizeToolName(claudeMcp[2]!));
    push("Request", request.summary);
  } else if (kind === "shell" && network) {
    capability = "Access the network";
    push("Network", `${str(network.host)}${str(network.protocol) ? ` (${str(network.protocol)})` : ""}`);
    push("Command", shellCommand({ input, summary: "" }));
    push("Folder", str(input.cwd));
  } else if (kind === "shell") {
    capability = KIND_CAPABILITY.shell;
    push("Command", shellCommand({ input, summary: "" }) || request.summary);
    push("Folder", str(input.cwd));
  } else if (kind === "edit") {
    capability = KIND_CAPABILITY.edit;
    const paths = filePaths(input);
    push("Files", paths.join("\n") || request.summary);
    push("Folder", str(input.grantRoot));
  } else if (request.tool === "permissions") {
    capability = "Extra access for this turn";
    reach.push(...grantedAccess(input));
    push("Folder", str(input.cwd));
    if (!reach.length) push("Request", request.summary);
  } else if (kind === "read" || kind === "image") {
    capability = KIND_CAPABILITY[kind];
    push("Files", filePaths(input).join("\n") || request.summary);
  } else if (kind === "web-fetch" || kind === "web-search" || kind === "files" || kind === "agent") {
    capability = KIND_CAPABILITY[kind];
    push("Request", request.summary);
  } else {
    capability = `Use ${humanizeToolName(request.tool).toLowerCase()}`;
    push("Request", request.summary);
  }
  const why = str(input.reason);
  if (why && why !== request.summary) push("Why", why);
  return { capability, reach };
}
