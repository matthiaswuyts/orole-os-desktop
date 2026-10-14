import type { Message, MsgPart } from "@/mygo";

/** What a tool/delegation row shows: the daemon's own status, except that a row still "running" in a settled message never finished. */
export type StepState = "running" | "done" | "error" | "stopped";

interface ToolWords { running: string; done: string; noun: string }

const TOOLS: Record<string, ToolWords> = {
  web_search: { running: "Searching the web", done: "Searched the web", noun: "Web search" },
  web_fetch: { running: "Reading a page", done: "Read a page", noun: "Page read" },
  datetime: { running: "Checking the date and time", done: "Checked the date and time", noun: "Date lookup" },
  research: { running: "Researching", done: "Researched", noun: "Research" },
  knowledge_search: { running: "Searching knowledge", done: "Searched knowledge", noun: "Knowledge search" },
  memory: { running: "Updating memory", done: "Updated memory", noun: "Memory update" },
  memory_search: { running: "Searching memory", done: "Searched memory", noun: "Memory search" },
  search_history: { running: "Searching this conversation", done: "Searched this conversation", noun: "History search" },
  save_profile: { running: "Writing my instructions", done: "Wrote my instructions", noun: "Instructions" },
  update_instructions: { running: "Updating my instructions", done: "Updated my instructions", noun: "Instructions" },
  orole_get_skill: { running: "Reading a skill", done: "Read a skill", noun: "Skill" },
  propose_skill: { running: "Drafting a skill", done: "Drafted a skill", noun: "Skill draft" },
};

const humanize = (name: string) => name.replace(/[_-]+/g, " ").trim() || "tool";

/** A tool's short name for chips ("Web search"); unknown ids are humanized. */
export function toolNoun(name: string): string {
  const noun = TOOLS[name]?.noun ?? humanize(name);
  return `${noun[0]!.toUpperCase()}${noun.slice(1)}`;
}

/** The row label for a tool id and state ("Searched the web", "Web search failed"). */
export function toolLabel(name: string | undefined, state: StepState): string {
  const words = TOOLS[name ?? ""] ?? (name?.startsWith("show_") ? { running: "Preparing a visual", done: "Showed a visual", noun: "Visual" } : undefined);
  const noun = words?.noun ?? humanize(name ?? "");
  switch (state) {
    case "running": return words?.running ?? `Using ${noun}`;
    case "done": return words?.done ?? `Used ${noun}`;
    case "error": return `${noun[0]!.toUpperCase()}${noun.slice(1)} failed`;
    case "stopped": return `${noun[0]!.toUpperCase()}${noun.slice(1)} did not finish`;
  }
}

/** Whether the message can still change (queued, streaming) as opposed to settled. */
export const isActive = (status: string | undefined): boolean => status === "running" || status === "pending";

/** The effective state of a step part given whether its message is still being produced. */
export function stepState(part: Pick<MsgPart, "status">, active: boolean): StepState {
  const status = (part.status ?? "").toLowerCase();
  if (status === "error" || status === "failed") return "error";
  if (status === "done" || status === "complete" || status === "completed" || status === "success") return "done";
  return active ? "running" : "stopped";
}

/** "820 ms", "4.2 s", "1 min 5 s". */
export function formatDuration(ms: number): string {
  if (ms < 1000) return `${Math.max(0, Math.round(ms))} ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)} s`;
  const total = Math.round(ms / 1000);
  return `${Math.floor(total / 60)} min ${total % 60} s`;
}

/** "12 KB", "3.4 MB". */
export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

/** The host of a URL for compact display; the raw text when it does not parse. */
export function hostOf(url: string): string {
  try { return new URL(url).host.replace(/^www\./, ""); } catch { return url; }
}

/** A source as shown in the sources popover. */
export interface Source { url: string; title: string; snippet?: string }

/** Distinct sources in order of first appearance, from citation parts. */
export function sourcesOf(parts: readonly MsgPart[]): Source[] {
  const seen = new Set<string>();
  const out: Source[] = [];
  for (const part of parts) {
    if (part.type !== "citation" || !part.url || seen.has(part.url)) continue;
    seen.add(part.url);
    out.push({ url: part.url, title: part.title?.trim() || hostOf(part.url), snippet: part.snippet });
  }
  return out;
}

/** The plain text of a message: its text parts, or the research report when that is all there is. */
export function messageText(message: Pick<Message, "parts">): string {
  const text = message.parts.filter((p) => p.type === "text" && p.text).map((p) => p.text).join("\n\n");
  if (text) return text;
  return message.parts.filter((p) => p.type === "report" && p.text).map((p) => p.text).join("\n\n");
}
