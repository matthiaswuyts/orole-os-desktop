import type { Event as AgentEvent, PermissionRequest, Session } from "@/mygo";

/**
 * Typed view of a session's event history. The Go side (internal/agents/types.go) stores one
 * `Event` per item with a kind-specific `data` payload; this module parses those payloads and groups
 * consecutive events into the items the task feed renders. Older events (the Claude adapter emitted
 * tool summaries as text only) have no `data`, so every parser tolerates a missing or odd payload.
 */

/** Event kinds that make up the readable conversation. Everything else (init, activity...) is ignored. */
export const FEED_KINDS: readonly string[] = ["prompt", "message", "reasoning", "tool", "tool_result", "image", "permission", "decision", "turn_done", "error", "system", "needs_input"];

export type ToolStatus = "running" | "ok" | "error" | "unknown";

export interface ToolCall {
  /** ToolData.id, or `event-<n>` for events written before tool ids existed. */
  id: string;
  /** True when the call carried a real id, so a result can be matched exactly. */
  linked: boolean;
  name: string;
  /** MCP server name; empty for the harness's built-in tools. */
  server: string;
  /** `unknown` means no result was ever recorded for the call (turn ended or session stopped). */
  status: ToolStatus;
  input: unknown;
  output: string;
  /** Adapter-provided one-line summary (Event.Text of the call). */
  summary: string;
}

export interface ImageInfo { id: string; path: string; mime: string; prompt: string }

interface Base { key: string; at: string }
export type FeedItem =
  | (Base & { type: "user"; text: string })
  | (Base & { type: "assistant"; text: string })
  | (Base & { type: "reasoning"; text: string; reasoningId: string })
  | (Base & { type: "tool"; call: ToolCall })
  | (Base & { type: "image"; image: ImageInfo })
  | (Base & { type: "permission"; request: PermissionRequest })
  | (Base & { type: "note"; kind: "system" | "decision" | "needs_input"; text: string })
  | (Base & { type: "error"; text: string })
  | (Base & { type: "turn"; ok: boolean; result: string; durationMs: number | null });

export const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);
const str = (v: unknown): string => typeof v === "string" ? v : "";

/** Event.data as an object; null, arrays and scalars (or a JSON string from an old build) become `{}`. */
export function dataOf(ev: Pick<AgentEvent, "data">): Record<string, unknown> {
  let data = ev.data;
  if (typeof data === "string") {
    try { data = JSON.parse(data); } catch { return {}; }
  }
  return isRecord(data) ? data : {};
}

/** Tool name and summary of a legacy `Name: summary` event text. */
function splitLegacyTool(text: string): { name: string; summary: string } {
  const at = text.indexOf(": ");
  return at > 0 && at <= 64 && !/\s/.test(text.slice(0, at)) ? { name: text.slice(0, at), summary: text.slice(at + 2) } : { name: "", summary: text };
}

const toolStatus = (v: unknown): ToolStatus => v === "ok" || v === "error" || v === "running" ? v : "running";

export function parseToolCall(ev: AgentEvent): ToolCall {
  const d = dataOf(ev);
  const id = str(d.id);
  const legacy = splitLegacyTool(ev.text);
  const name = str(d.name) || str(d.tool) || legacy.name || "tool";
  return { id: id || `event-${ev.id}`, linked: id !== "", name, server: str(d.server), status: toolStatus(d.status), input: d.input, output: str(d.output), summary: legacy.summary };
}

/** A tool result as a partial update to its call. `linked` is false for results without ToolData. */
function parseToolResult(ev: AgentEvent): { id: string; linked: boolean; status: ToolStatus; output: string; call: ToolCall } {
  const d = dataOf(ev);
  const id = str(d.id);
  // Legacy results carried only {error: bool}; the summary text was the output.
  const status: ToolStatus = d.status === "error" || d.error === true ? "error" : "ok";
  const output = str(d.output) || ev.text;
  const call: ToolCall = { id: id || `event-${ev.id}`, linked: id !== "", name: str(d.name) || "tool", server: str(d.server), status, input: d.input, output, summary: "" };
  return { id, linked: id !== "", status, output, call };
}

export function parseImage(ev: AgentEvent): ImageInfo | null {
  const d = dataOf(ev);
  const path = str(d.path);
  if (!path) return null;
  return { id: str(d.id) || `event-${ev.id}`, path, mime: str(d.mime) || "image/png", prompt: str(d.prompt) || ev.text };
}

/** The permission request an event carries, filling gaps for events without full data. */
export function parsePermission(ev: AgentEvent): PermissionRequest {
  const d = dataOf(ev) as Partial<PermissionRequest>;
  return { id: d.id ?? String(ev.id), tool: d.tool ?? ev.text, summary: d.summary ?? ev.text, at: d.at ?? ev.at, input: d.input, inputJSON: d.inputJSON ?? "" };
}

export interface TurnDone { ok: boolean; result: string }
export function parseTurnDone(ev: AgentEvent): TurnDone {
  const d = dataOf(ev);
  return { ok: d.ok !== false, result: str(d.result) || ev.text };
}

/** Merges one event into an id-sorted list; an already-known id is ignored. */
export function mergeEvent(list: AgentEvent[], ev: AgentEvent): AgentEvent[] {
  if (!FEED_KINDS.includes(ev.kind) || list.some((x) => x.id === ev.id)) return list;
  const last = list[list.length - 1];
  return !last || last.id < ev.id ? [...list, ev] : [...list, ev].sort((a, b) => a.id - b.id);
}

/** Merges loaded history with events that arrived while it loaded. */
export function mergeHistory(history: AgentEvent[], extra: AgentEvent[]): AgentEvent[] {
  const all = new Map<number, AgentEvent>();
  for (const ev of [...history, ...extra]) if (FEED_KINDS.includes(ev.kind)) all.set(ev.id, ev);
  return [...all.values()].sort((a, b) => a.id - b.id);
}

export interface GroupOptions {
  /** Whether the agent process is alive: a call without a result of a dead session is `unknown`, not `running`. */
  live: boolean;
}

/**
 * Groups id-sorted events into feed items: tool results fold into their call (by id, or in order for
 * legacy events), a reasoning event with a repeated id replaces the earlier one, and turn_done
 * becomes a separator carrying the turn's duration.
 */
export function groupEvents(events: AgentEvent[], options: GroupOptions = { live: true }): FeedItem[] {
  const items: FeedItem[] = [];
  const toolAt = new Map<string, number>(); // ToolData.id -> index in items
  const reasoningAt = new Map<string, number>();
  const legacyOpen: number[] = []; // legacy calls still waiting for a result, oldest first
  let turnStart: number | null = null;

  const settleOpen = () => {
    for (const [i, item] of items.entries()) if (item.type === "tool" && item.call.status === "running") items[i] = { ...item, call: { ...item.call, status: "unknown" } };
    legacyOpen.length = 0;
  };

  for (const ev of events) {
    const key = String(ev.id);
    switch (ev.kind) {
      case "prompt":
        turnStart = Date.parse(ev.at);
        items.push({ type: "user", key, at: ev.at, text: ev.text });
        break;
      case "message":
        if (ev.text.trim()) items.push({ type: "assistant", key, at: ev.at, text: ev.text });
        break;
      case "reasoning": {
        const reasoningId = str(dataOf(ev).id);
        const prior = reasoningId ? reasoningAt.get(reasoningId) : undefined;
        const item: FeedItem = { type: "reasoning", key: prior === undefined ? key : items[prior]!.key, at: ev.at, text: ev.text, reasoningId };
        if (prior !== undefined) items[prior] = item;
        else { if (reasoningId) reasoningAt.set(reasoningId, items.length); items.push(item); }
        break;
      }
      case "tool": {
        const call = parseToolCall(ev);
        const prior = call.linked ? toolAt.get(call.id) : undefined;
        if (prior !== undefined) { // the adapter re-announced a call (e.g. input completed)
          const old = items[prior] as Extract<FeedItem, { type: "tool" }>;
          items[prior] = { ...old, call: { ...old.call, ...call, status: old.call.status === "running" ? call.status : old.call.status, output: old.call.output || call.output } };
          break;
        }
        if (call.linked) toolAt.set(call.id, items.length);
        else if (call.status === "running") legacyOpen.push(items.length);
        items.push({ type: "tool", key, at: ev.at, call });
        break;
      }
      case "tool_result": {
        const res = parseToolResult(ev);
        const index = res.linked ? toolAt.get(res.id) : legacyOpen.shift();
        const existing = index === undefined ? undefined : items[index];
        if (index !== undefined && existing?.type === "tool") {
          items[index] = { ...existing, call: { ...existing.call, status: res.status, output: res.output || existing.call.output } };
        } else {
          if (res.linked) toolAt.set(res.id, items.length);
          items.push({ type: "tool", key, at: ev.at, call: res.call });
        }
        break;
      }
      case "image": {
        const image = parseImage(ev);
        if (image) items.push({ type: "image", key, at: ev.at, image });
        else items.push({ type: "note", key, at: ev.at, kind: "system", text: ev.text || "An image was generated but its file is unavailable." });
        break;
      }
      case "permission":
        items.push({ type: "permission", key, at: ev.at, request: parsePermission(ev) });
        break;
      case "decision":
      case "system":
      case "needs_input":
        items.push({ type: "note", key, at: ev.at, kind: ev.kind, text: ev.text || (ev.kind === "needs_input" ? "Waiting for you." : "") });
        break;
      case "error":
        items.push({ type: "error", key, at: ev.at, text: ev.text });
        break;
      case "turn_done": {
        const turn = parseTurnDone(ev);
        settleOpen();
        const end = Date.parse(ev.at);
        const durationMs = turnStart !== null && Number.isFinite(end) && Number.isFinite(turnStart) && end >= turnStart ? end - turnStart : null;
        items.push({ type: "turn", key, at: ev.at, ok: turn.ok, result: turn.result, durationMs });
        turnStart = null;
        break;
      }
      default:
        break;
    }
  }
  if (!options.live) settleOpen();
  return items;
}

/** What the agent is doing right now, from Session.activity, for the live indicator under the feed. */
export interface LiveActivity { kind: "thinking" | "working"; label: string }
export function liveActivity(session: Pick<Session, "status" | "live" | "activity">): LiveActivity | null {
  if (!session.live || session.status !== "working") return null;
  const activity = session.activity.trim();
  if (/^(thinking|reasoning)\b/i.test(activity)) return { kind: "thinking", label: "Thinking…" };
  return { kind: "working", label: activity || "Working…" };
}

const PREVIEW_KEYS = ["command", "cmd", "file_path", "path", "pattern", "query", "url", "prompt", "description"];
/** Collapses whitespace and cuts to n characters with an ellipsis. */
export const clip = (s: string, n: number) => { const one = s.replace(/\s+/g, " ").trim(); return one.length > n ? `${one.slice(0, n - 1)}…` : one; };

/** One-line description of a tool call for the collapsed card. */
export function toolPreview(call: ToolCall): string {
  if (call.summary) return clip(call.summary, 140);
  if (isRecord(call.input)) {
    for (const k of PREVIEW_KEYS) if (typeof call.input[k] === "string" && call.input[k]) return clip(call.input[k], 140);
    const json = JSON.stringify(call.input);
    return json === "{}" ? "" : clip(json, 140);
  }
  return typeof call.input === "string" ? clip(call.input, 140) : "";
}

/** The tool input as indented JSON for the expanded card; "" when the call had no input data. */
export function toolInputJSON(call: ToolCall): string {
  if (call.input === undefined || call.input === null) return "";
  try { return JSON.stringify(call.input, null, 2); } catch { return String(call.input); }
}
