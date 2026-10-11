import { Bot, FileSearch, FileText, Globe, Image as ImageIcon, Pencil, Plug, Terminal, Wrench, type LucideIcon } from "lucide-react";
import { clip, isRecord, type ToolCall } from "./taskEvents";

/**
 * Plain-language labels for tool calls. Harnesses report tools by their internal names and raw JSON input
 * (`shell {"command":"\"C:\\...\\pwsh.exe\" -Command '...'"}`, `web_search {"action":null,"query":""}`); this module turns a
 * call into the `verb` + `chip` a regular user can read. The raw name, input and output stay in the row's details.
 */

export type ToolKind = "shell" | "web-search" | "web-fetch" | "read" | "edit" | "files" | "image" | "agent" | "mcp" | "other";
export interface ToolFileStat { file: string; added?: number; removed?: number }
export interface ToolLabel {
  kind: ToolKind;
  /** "Ran", "Searched the web"... (present participle while the call is running). */
  verb: string;
  /** The one thing worth showing: the command, the query, the file name. May be empty. */
  chip: string;
  /** The untruncated chip text for a tooltip. */
  title: string;
  icon: LucideIcon;
  stats: ToolFileStat[];
}

const ICONS: Record<ToolKind, LucideIcon> = { shell: Terminal, "web-search": Globe, "web-fetch": Globe, read: FileText, edit: Pencil, files: FileSearch, image: ImageIcon, agent: Bot, mcp: Plug, other: Wrench };
const VERBS: Record<ToolKind, readonly [running: string, done: string] | null> = {
  shell: ["Running", "Ran"], "web-search": ["Searching the web", "Searched the web"], "web-fetch": ["Fetching", "Fetched"], read: ["Reading", "Read"],
  edit: ["Editing", "Edited"], files: ["Searching files", "Searched files"], image: ["Viewing", "Viewed"], agent: ["Asking an agent", "Asked an agent"], mcp: ["Using", "Used"], other: null,
};

const norm = (name: string) => name.toLowerCase().replace(/[^a-z0-9]+/g, "_").replace(/^_+|_+$/g, "");
const KINDS: Record<string, ToolKind> = {};
for (const [kind, names] of Object.entries({
  shell: ["shell", "bash", "command", "cmd", "exec", "exec_command", "run_command", "local_shell", "powershell", "pwsh", "terminal", "execute", "execute_command", "run_terminal_cmd", "run_shell_command", "bash_command"],
  "web-search": ["web_search", "websearch", "search_web", "google_search"],
  "web-fetch": ["web_fetch", "webfetch", "fetch", "fetch_url", "browse"],
  read: ["read", "read_file", "readfile", "view", "open", "open_file", "cat", "read_many_files"],
  image: ["view_image"],
  edit: ["edit", "multiedit", "multi_edit", "apply_patch", "applypatch", "patch", "write", "write_file", "writefile", "create", "create_file", "str_replace", "str_replace_editor", "str_replace_based_edit_tool", "notebookedit", "notebook_edit", "replace"],
  files: ["grep", "glob", "find", "search", "search_files", "ls", "list", "list_files", "list_dir", "list_directory", "rg", "codebase_search", "find_files", "search_file_content"],
  agent: ["task", "agent", "subagent"],
} satisfies Record<string, string[]>)) for (const name of names) KINDS[name] = kind as ToolKind;

/** `mcp__server__tool` names that arrive without a separate server field. */
function splitMcpName(name: string): { server: string; tool: string } | null {
  const rest = name.startsWith("mcp__") ? name.slice(5) : "";
  const at = rest.indexOf("__");
  return at > 0 && at < rest.length - 2 ? { server: rest.slice(0, at), tool: rest.slice(at + 2) } : null;
}

export function toolKind(name: string, server = ""): ToolKind {
  if (server || splitMcpName(name)) return "mcp";
  const key = norm(name);
  return KINDS[key] ?? (key.startsWith("agent_") ? "agent" : "other");
}

/** "view_image" -> "View image", "WebFetch" -> "Web fetch". */
export function humanizeToolName(name: string): string {
  const words = name.replace(/([a-z0-9])([A-Z])/g, "$1 $2").replace(/[_\-.:/]+/g, " ").replace(/\s+/g, " ").trim().toLowerCase();
  return words ? words[0]!.toUpperCase() + words.slice(1) : "Tool";
}

export function basename(path: string): string {
  const parts = path.split(/[\\/]+/).filter(Boolean);
  return parts[parts.length - 1] ?? path;
}

const str = (v: unknown): string => typeof v === "string" ? v : "";
const strings = (v: unknown): string[] => Array.isArray(v) ? v.filter((s): s is string => typeof s === "string") : [];
const first = (input: unknown, keys: readonly string[]): string => {
  if (!isRecord(input)) return "";
  for (const key of keys) { const value = str(input[key]).trim(); if (value) return value; }
  return "";
};
/** An adapter summary is only usable as text when it is not just the serialized input. */
const plainSummary = (call: Pick<ToolCall, "summary">) => /^\s*[{[]/.test(call.summary) ? "" : call.summary.trim();

// ---- shell ----

const WRAPPER = /^(pwsh|powershell|cmd|bash|sh|zsh|dash|fish)$/;
const PS_SWITCHES: Record<string, true> = { "-nologo": true, "-nol": true, "-noprofile": true, "-nop": true, "-noninteractive": true, "-noni": true, "-noexit": true, "-mta": true, "-sta": true };
const PS_VALUES: Record<string, true> = { "-executionpolicy": true, "-ep": true, "-windowstyle": true, "-w": true, "-workingdirectory": true, "-wd": true, "-outputformat": true, "-inputformat": true, "-configurationname": true };

function readToken(text: string): { token: string; rest: string; quoted: boolean } | null {
  const s = text.trimStart();
  if (!s) return null;
  const q = s[0]!;
  if (q === '"' || q === "'") {
    let i = 1;
    while (i < s.length && s[i] !== q) i += q === '"' && s[i] === "\\" && s[i + 1] === q ? 2 : 1;
    return { token: s.slice(1, i), rest: s.slice(i + 1), quoted: true };
  }
  const word = /^\S+/.exec(s)![0];
  return { token: word, rest: s.slice(word.length), quoted: false };
}

/** The script of `pwsh -Command '<script>'`, `cmd /c <script>`, `bash -lc "<script>"`, with one layer of quoting removed. */
function script(rest: string, depth: number): string {
  const s = rest.trim();
  const q = s[0];
  const inner = (q === '"' || q === "'") && s.length >= 2 && s.endsWith(q) ? (q === '"' ? s.slice(1, -1).replace(/\\"/g, '"') : s.slice(1, -1)) : s;
  return unwrapShellCommand(inner, depth + 1);
}

/** Strips a PowerShell/cmd/POSIX shell wrapper from a command line; any other command is returned untouched. */
export function unwrapShellCommand(command: string, depth = 0): string {
  const line = command.trim();
  const head = depth > 3 ? null : readToken(line);
  if (!head) return line;
  const exe = basename(head.token).toLowerCase().replace(/\.exe$/, "");
  if (!WRAPPER.test(exe)) return line;
  const family = exe === "pwsh" || exe === "powershell" ? "powershell" : exe === "cmd" ? "cmd" : "posix";
  let rest = head.rest;
  for (;;) {
    const next = readToken(rest);
    if (!next || next.quoted) return line;
    const flag = next.token.toLowerCase();
    if (family === "powershell") {
      if (flag === "-command" || flag === "-c") return script(next.rest, depth);
      if (Object.hasOwn(PS_SWITCHES, flag)) { rest = next.rest; continue; }
      if (Object.hasOwn(PS_VALUES, flag)) { const value = readToken(next.rest); if (!value) return line; rest = value.rest; continue; }
      return line;
    }
    if (family === "cmd") {
      if (flag === "/c" || flag === "/k") return script(next.rest, depth);
      if (/^\/[a-z](:\S+)?$/.test(flag)) { rest = next.rest; continue; }
      return line;
    }
    if (/^-[a-z]*c$/.test(flag)) return script(next.rest, depth);
    if (flag.startsWith("-")) { rest = next.rest; continue; }
    return line;
  }
}

const quoteArg = (arg: string) => /[\s"]/.test(arg) || arg === "" ? `"${arg.replace(/"/g, '\\"')}"` : arg;

/** The command line a shell tool call ran, as the harness reported it (wrapper included). */
export function shellCommand(call: Pick<ToolCall, "input" | "summary">): string {
  const { input } = call;
  if (typeof input === "string") return input;
  if (Array.isArray(input) && input.length && input.every((s) => typeof s === "string")) return (input as string[]).map(quoteArg).join(" ");
  if (isRecord(input)) {
    for (const key of ["command", "cmd", "commandLine", "script"]) {
      const value = input[key];
      if (typeof value === "string" && value.trim()) return value;
      const argv = strings(value);
      if (argv.length) return argv.map(quoteArg).join(" ");
    }
  }
  return plainSummary(call);
}

// ---- edits ----

const lineCount = (text: string) => text === "" ? 0 : text.replace(/\r?\n$/, "").split(/\r?\n/).length;

function diffStats(diff: string): { added: number; removed: number } | null {
  let added = 0, removed = 0;
  for (const line of diff.split(/\r?\n/)) {
    if (line.startsWith("+") && !line.startsWith("+++")) added++;
    else if (line.startsWith("-") && !line.startsWith("---")) removed++;
  }
  return added || removed ? { added, removed } : null;
}

interface FileEdit { path: string; added?: number; removed?: number }
const PATH_KEYS = ["file_path", "filePath", "path", "file", "target_file", "notebook_path", "filename"] as const;

/** Files changed by `*** Update File: x` style patch text, with per-file +/- counts. */
function patchEdits(patch: string): FileEdit[] {
  const edits: FileEdit[] = [];
  let current: FileEdit | null = null;
  for (const line of patch.split(/\r?\n/)) {
    const header = /^\*\*\* (?:Add|Update|Delete) File: (.+)$/.exec(line);
    if (header) { current = { path: header[1]!.trim(), added: 0, removed: 0 }; edits.push(current); continue; }
    if (!current) continue;
    if (line.startsWith("+") && !line.startsWith("+++")) current.added = (current.added ?? 0) + 1;
    else if (line.startsWith("-") && !line.startsWith("---")) current.removed = (current.removed ?? 0) + 1;
  }
  return edits;
}

function editsOf(call: Pick<ToolCall, "input" | "summary">): FileEdit[] {
  const { input } = call;
  if (typeof input === "string") return patchEdits(input);
  if (!isRecord(input)) { const path = plainSummary(call); return path ? [{ path }] : []; }
  if (Array.isArray(input.changes)) {
    const edits = input.changes.filter(isRecord).flatMap((change): FileEdit[] => {
      const path = str(change.path);
      if (!path) return [];
      const stats = diffStats(str(change.diff));
      return [{ path, ...(stats ?? {}) }];
    });
    if (edits.length) return edits;
  }
  const patch = first(input, ["patch", "input", "diff"]);
  if (patch) { const edits = patchEdits(patch); if (edits.length) return edits; }
  const path = first(input, PATH_KEYS);
  if (!path) return [];
  const pairs = Array.isArray(input.edits) ? input.edits.filter(isRecord) : [input];
  let added: number | undefined, removed: number | undefined;
  for (const edit of pairs) {
    const next = first(edit, ["new_string", "newString", "new_str"]), prev = first(edit, ["old_string", "oldString", "old_str"]);
    if (next || prev) { added = (added ?? 0) + lineCount(next); removed = (removed ?? 0) + lineCount(prev); }
  }
  const content = first(input, ["content", "file_text", "text"]);
  if (added === undefined && content) added = lineCount(content);
  const explicitAdded = input.added ?? input.additions, explicitRemoved = input.removed ?? input.deletions;
  if (typeof explicitAdded === "number") added = explicitAdded;
  if (typeof explicitRemoved === "number") removed = explicitRemoved;
  return [{ path, ...(added !== undefined ? { added } : {}), ...(removed !== undefined ? { removed } : {}) }];
}

function fileChip(paths: readonly string[]): string {
  const names = [...new Set(paths.map(basename))];
  return names.length <= 3 ? names.join(", ") : `${names.slice(0, 2).join(", ")}, +${names.length - 2} more`;
}

// ---- the label ----

const PREVIEW_KEYS = ["command", "cmd", "file_path", "path", "pattern", "query", "url", "prompt", "description"] as const;
const CHIP_LIMIT = 100;

const webSearchQuery = (input: unknown): string => {
  if (!isRecord(input)) return typeof input === "string" ? input.trim() : "";
  const direct = first(input, ["query", "q", "search_query", "searchQuery"]);
  if (direct) return direct;
  const nested = [input.action, input].flatMap((source) => isRecord(source) ? [first(source, ["query", "q"]), strings(source.queries)[0] ?? ""] : []);
  return nested.find(Boolean) ?? "";
};

/** `{verb, chip, icon}` for one tool call: what a regular user should read instead of the raw name and JSON. */
export function toolLabel(call: Pick<ToolCall, "name" | "server" | "input" | "summary" | "status">): ToolLabel {
  const mcp = splitMcpName(call.name);
  const server = call.server || mcp?.server || "";
  const name = mcp?.tool ?? call.name;
  const kind = toolKind(call.name, call.server);
  const verbs = VERBS[kind];
  const running = call.status === "running";
  let verb = verbs ? verbs[running ? 0 : 1] : humanizeToolName(name);
  let title = "";
  let stats: ToolFileStat[] = [];

  switch (kind) {
    case "shell": {
      const full = unwrapShellCommand(shellCommand(call));
      title = full;
      const line = full.split(/\r?\n/).map((l) => l.trim()).find(Boolean) ?? "";
      return { kind, verb, chip: clip(line, CHIP_LIMIT), title, icon: ICONS[kind], stats };
    }
    case "web-search": title = webSearchQuery(call.input) || plainSummary(call); break;
    case "web-fetch": title = first(call.input, ["url", "uri"]) || plainSummary(call); break;
    case "read": case "image": title = first(call.input, PATH_KEYS) || strings(isRecord(call.input) ? call.input.paths : undefined)[0] || plainSummary(call); break;
    case "files": title = first(call.input, ["pattern", "query", "regex", "glob_pattern", "file_pattern", "path"]) || plainSummary(call); break;
    case "edit": {
      const edits = editsOf(call);
      title = edits.map((edit) => edit.path).join(", ");
      stats = edits.filter((edit) => edit.added !== undefined || edit.removed !== undefined).map((edit) => ({ file: basename(edit.path), ...(edit.added !== undefined ? { added: edit.added } : {}), ...(edit.removed !== undefined ? { removed: edit.removed } : {}) }));
      return { kind, verb, chip: fileChip(edits.map((edit) => edit.path)), title, icon: ICONS[kind], stats };
    }
    case "agent": title = first(call.input, ["description", "prompt", "task"]) || plainSummary(call); break;
    case "mcp": title = `${server || "MCP"} · ${humanizeToolName(name)}`; verb = verbs![running ? 0 : 1]; break;
    default: {
      title = first(call.input, PREVIEW_KEYS) || plainSummary(call);
    }
  }
  const chip = kind === "read" || kind === "image" ? (title ? basename(title) : "") : kind === "web-fetch" ? clip(title.replace(/^https?:\/\//, ""), CHIP_LIMIT) : clip(title, CHIP_LIMIT);
  return { kind, verb, chip, title, icon: ICONS[kind], stats };
}

/** "Ran npm test": the one-line text of a tool step, for labels and accessible names. */
export const toolLabelText = (label: Pick<ToolLabel, "verb" | "chip">) => label.chip ? `${label.verb} ${label.chip}` : label.verb;
