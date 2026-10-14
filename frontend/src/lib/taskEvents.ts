import type { Event as AgentEvent, PermissionRequest, Question, QuestionAnswer, QuestionRequest, Session } from "@/mygo";
import type { FileData, GuardrailData, ImageStatusData, SourceReference, SubagentData } from "./taskChatTypes";

/**
 * Typed view of a session's event history. The Go side (internal/agents/types.go) stores one
 * `Event` per item with a kind-specific `data` payload; this module parses those payloads and groups
 * consecutive events into the items the task feed renders. Older events (the Claude adapter emitted
 * tool summaries as text only) have no `data`, so every parser tolerates a missing or odd payload.
 */

/** Event kinds that make up the readable conversation. Everything else (init, activity...) is ignored. */
export const FEED_KINDS: readonly string[] = ["prompt", "message", "reasoning", "tool", "tool_result", "image", "permission", "decision", "turn_done", "error", "system", "needs_input", "usage", "question", "question_cancel", "answer", "sources", "file", "image_status", "subagent", "guardrail"];

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
  /** When the call was announced and when its result arrived (Unix ms); absent when the history does not say. */
  startedAt?: number;
  endedAt?: number;
}

export interface ImageInfo { id: string; path: string; mime: string; prompt: string }

interface Base { key: string; at: string }
export type FeedItem =
  | (Base & { type: "user"; text: string })
  | (Base & { type: "assistant"; text: string })
  | (Base & { type: "reasoning"; text: string; reasoningId: string })
  | (Base & { type: "tool"; call: ToolCall })
  | (Base & { type: "image"; image: ImageInfo })
  | (Base & { type: "permission"; request: PermissionRequest; decision?: { allow: boolean; reason: string } })
  | (Base & { type: "sources"; sources: SourceReference[] })
  | (Base & { type: "file"; file: FileData })
  | (Base & { type: "generation"; generation: ImageStatusData; image?: ImageInfo })
  | (Base & { type: "subagent"; agent: SubagentData })
  | (Base & { type: "guardrail"; notice: GuardrailData })
  | (Base & { type: "question"; request: QuestionRequest; answers?: QuestionAnswer[]; cancelled?: boolean })
  | (Base & { type: "note"; kind: "system" | "decision" | "needs_input"; text: string })
  | (Base & { type: "error"; text: string })
  | (Base & { type: "turn"; ok: boolean; result: string; durationMs: number | null });

export const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);
const str = (v: unknown): string => typeof v === "string" ? v : "";
/** An event timestamp as Unix ms; undefined when it does not parse. */
const msOf = (iso: string): number | undefined => { const t = Date.parse(iso); return Number.isFinite(t) ? t : undefined; };

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
  return { id: id || `event-${ev.id}`, linked: id !== "", name, server: str(d.server), status: toolStatus(d.status), input: d.input, output: str(d.output), summary: legacy.summary, startedAt: msOf(ev.at) };
}

/** A tool result as a partial update to its call. `linked` is false for results without ToolData. */
function parseToolResult(ev: AgentEvent): { id: string; linked: boolean; status: ToolStatus; output: string; call: ToolCall } {
  const d = dataOf(ev);
  const id = str(d.id);
  // Legacy results carried only {error: bool}; the summary text was the output.
  const status: ToolStatus = d.status === "error" || d.error === true ? "error" : "ok";
  const output = str(d.output) || ev.text;
  const call: ToolCall = { id: id || `event-${ev.id}`, linked: id !== "", name: str(d.name) || "tool", server: str(d.server), status, input: d.input, output, summary: "", endedAt: msOf(ev.at) };
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

function parseQuestion(ev: AgentEvent): QuestionRequest | null {
  const d = dataOf(ev);
  if (!str(d.id) || !Array.isArray(d.questions)) return null;
  const questions: Question[] = d.questions.filter(isRecord).filter((q) => str(q.id) && str(q.prompt)).map((q) => ({
    id: str(q.id), header: str(q.header), prompt: str(q.prompt),
    options: Array.isArray(q.options) ? q.options.filter(isRecord).filter((o) => str(o.id) && str(o.label)).map((o) => ({ id: str(o.id), label: str(o.label), description: str(o.description) })) : [],
    multiple: q.multiple === true, allowText: q.allowText === true, secret: q.secret === true,
  }));
  return questions.length ? { id: str(d.id), questions, at: str(d.at) || ev.at, ...(str(d.expiresAt) ? { expiresAt: str(d.expiresAt) } : {}) } : null;
}

function parseAnswers(value: unknown): QuestionAnswer[] {
  return Array.isArray(value) ? value.filter(isRecord).filter((a) => str(a.questionId) && Array.isArray(a.values)).map((a) => ({
    questionId: str(a.questionId), values: (a.values as unknown[]).filter((v): v is string => typeof v === "string"),
  })) : [];
}

export interface TurnDone { ok: boolean; result: string }
export function parseTurnDone(ev: AgentEvent): TurnDone {
  const d = dataOf(ev);
  return { ok: d.ok !== false, result: str(d.result) || ev.text };
}

const FEED_KIND: Record<string, true> = Object.fromEntries(FEED_KINDS.map((kind) => [kind, true]));

/** Merges one event into an id-sorted list; an already-known id is ignored. Appending the newest event is O(1) to locate; anything older binary-searches. */
export function mergeEvent(list: AgentEvent[], ev: AgentEvent): AgentEvent[] {
  if (!FEED_KIND[ev.kind]) return list;
  const last = list[list.length - 1];
  if (!last || last.id < ev.id) return [...list, ev];
  let lo = 0, hi = list.length;
  while (lo < hi) {
    const mid = (lo + hi) >>> 1;
    if (list[mid]!.id < ev.id) lo = mid + 1; else hi = mid;
  }
  if (list[lo]!.id === ev.id) return list;
  return [...list.slice(0, lo), ev, ...list.slice(lo)];
}

/** Merges loaded history with events that arrived while it loaded; on a repeated id the later event wins. */
export function mergeHistory(history: AgentEvent[], extra: AgentEvent[]): AgentEvent[] {
  const all = new Map<number, AgentEvent>();
  for (const ev of history) if (FEED_KIND[ev.kind]) all.set(ev.id, ev);
  for (const ev of extra) if (FEED_KIND[ev.kind]) all.set(ev.id, ev);
  const merged = [...all.values()];
  for (let i = 1; i < merged.length; i++) if (merged[i - 1]!.id > merged[i]!.id) return merged.sort((a, b) => a.id - b.id);
  return merged;
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
  const correlatedAt = new Map<string, number>();
  const decisions = new Map<string, { allow: boolean; reason: string }>();
  const answers = new Map<string, { answers?: QuestionAnswer[]; cancelled: boolean }>();
  const permissionIds = new Set<string>();
  // Resolving events can precede requests in a partial/replayed history.
  for (const ev of events) {
    const d = dataOf(ev), id = str(d.id);
    if (ev.kind === "permission") permissionIds.add(parsePermission(ev).id);
    if (!id) continue;
    if (ev.kind === "decision" && typeof d.allow === "boolean") decisions.set(id, { allow: d.allow, reason: str(d.reason) });
    if (ev.kind === "answer") answers.set(id, { answers: parseAnswers(d.answers), cancelled: d.cancelled === true });
    if (ev.kind === "question_cancel") answers.set(id, { cancelled: true });
  }
  const upsert = (identity: string, item: FeedItem) => {
    const index = correlatedAt.get(identity);
    if (index === undefined) { correlatedAt.set(identity, items.length); items.push(item); }
    else items[index] = { ...item, key: items[index]!.key, at: items[index]!.at };
  };
  const legacyOpen: number[] = []; // legacy calls still waiting for a result, oldest first
  const running: number[] = []; // indexes of tool items announced as running; settled when a turn ends
  let turnStart: number | null = null;

  const settleOpen = () => {
    for (const i of running) {
      const item = items[i];
      if (item?.type === "tool" && item.call.status === "running") items[i] = { ...item, call: { ...item.call, status: "unknown" } };
    }
    running.length = 0;
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
        const item: FeedItem = { type: "reasoning", key: prior === undefined ? key : items[prior]!.key, at: prior === undefined ? ev.at : items[prior]!.at, text: ev.text, reasoningId };
        if (prior !== undefined) items[prior] = item;
        else { if (reasoningId) reasoningAt.set(reasoningId, items.length); items.push(item); }
        break;
      }
      case "tool": {
        const call = parseToolCall(ev);
        const prior = call.linked ? toolAt.get(call.id) : undefined;
        if (prior !== undefined) { // the adapter re-announced a call (e.g. input completed)
          const old = items[prior] as Extract<FeedItem, { type: "tool" }>;
          const status = old.call.status === "running" ? call.status : old.call.status;
          const endedAt = old.call.endedAt ?? (old.call.status === "running" && status !== "running" ? call.startedAt : undefined);
          items[prior] = { ...old, call: { ...old.call, ...call, status, output: old.call.output || call.output, startedAt: old.call.startedAt ?? call.startedAt, endedAt } };
          break;
        }
        if (call.linked) toolAt.set(call.id, items.length);
        else if (call.status === "running") legacyOpen.push(items.length);
        if (call.status === "running") running.push(items.length);
        items.push({ type: "tool", key, at: ev.at, call });
        break;
      }
      case "tool_result": {
        const res = parseToolResult(ev);
        const index = res.linked ? toolAt.get(res.id) : legacyOpen.shift();
        const existing = index === undefined ? undefined : items[index];
        if (index !== undefined && existing?.type === "tool") {
          items[index] = { ...existing, call: { ...existing.call, status: res.status, output: res.output || existing.call.output, endedAt: res.call.endedAt } };
        } else {
          if (res.linked) toolAt.set(res.id, items.length);
          items.push({ type: "tool", key, at: ev.at, call: res.call });
        }
        break;
      }
      case "image": {
        const image = parseImage(ev);
        if (image) {
          const identity = `image:${image.id}`;
          const index = correlatedAt.get(identity);
          const prior = index === undefined ? undefined : items[index];
          upsert(identity, prior?.type === "generation" ? { ...prior, image } : { type: "image", key, at: ev.at, image });
        } else items.push({ type: "note", key, at: ev.at, kind: "system", text: ev.text || "An image was generated but its file is unavailable." });
        break;
      }
      case "image_status": {
        const d = dataOf(ev), id = str(d.id);
        if (!id || !["running", "complete", "failed"].includes(str(d.status))) break;
        const identity = `image:${id}`, index = correlatedAt.get(identity);
        const prior = index === undefined ? undefined : items[index];
        const generation: ImageStatusData = { id, status: str(d.status), prompt: str(d.prompt), error: str(d.error) };
        upsert(identity, { type: "generation", key, at: ev.at, generation, image: prior?.type === "image" || prior?.type === "generation" ? prior.image : undefined });
        break;
      }
      case "permission": {
        const request = parsePermission(ev);
        upsert(`permission:${request.id}`, { type: "permission", key, at: ev.at, request, decision: decisions.get(request.id) });
        break;
      }
      case "question": {
        const request = parseQuestion(ev);
        if (request) upsert(`question:${request.id}`, { type: "question", key, at: ev.at, request, ...answers.get(request.id) });
        break;
      }
      case "sources": {
        const d = dataOf(ev);
        const sources: SourceReference[] = Array.isArray(d.items) ? d.items.filter(isRecord).filter((s) => str(s.url)).map((s) => ({ id: str(s.id), title: str(s.title), url: str(s.url), snippet: str(s.snippet) })) : [];
        if (sources.length) upsert(`sources:${str(d.id) || key}`, { type: "sources", key, at: ev.at, sources });
        break;
      }
      case "file": {
        const d = dataOf(ev);
        if (!str(d.path)) break;
        const file: FileData = { id: str(d.id), path: str(d.path), name: str(d.name), mime: str(d.mime), status: str(d.status), summary: str(d.summary) };
        upsert(`file:${file.id || key}`, { type: "file", key, at: ev.at, file });
        break;
      }
      case "subagent": {
        const d = dataOf(ev);
        if (!str(d.id)) break;
        const agent: SubagentData = { id: str(d.id), name: str(d.name), status: str(d.status), task: str(d.task), message: str(d.message), toolCallId: str(d.toolCallId) };
        upsert(`subagent:${agent.id}`, { type: "subagent", key, at: ev.at, agent });
        break;
      }
      case "guardrail": {
        const d = dataOf(ev);
        if (!str(d.message) && !ev.text) break;
        const notice: GuardrailData = { id: str(d.id), kind: str(d.kind), message: str(d.message) || ev.text, source: str(d.source) };
        upsert(`guardrail:${notice.id || key}`, { type: "guardrail", key, at: ev.at, notice });
        break;
      }
      case "decision":
        // Unmatched decisions remain readable when the request is outside the loaded page.
        if (permissionIds.has(str(dataOf(ev).id)) && typeof dataOf(ev).allow === "boolean") break;
        items.push({ type: "note", key, at: ev.at, kind: "decision", text: ev.text });
        break;
      case "system":
        // Older transcripts recorded a "Permission mode changed to …" note; the mode picker shows it now.
        if (ev.text.startsWith("Permission mode changed to ")) break;
        items.push({ type: "note", key, at: ev.at, kind: ev.kind, text: ev.text });
        break;
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

function sameValue(a: unknown, b: unknown): boolean {
  if (Object.is(a, b)) return true;
  if (Array.isArray(a)) return Array.isArray(b) && a.length === b.length && a.every((x, i) => sameValue(x, b[i]));
  if (!isRecord(a) || !isRecord(b)) return false;
  const keys = Object.keys(a);
  return keys.length === Object.keys(b).length && keys.every((k) => k in b && sameValue(a[k], b[k]));
}

/**
 * Reuses the item objects of `previous` wherever the regrouped `next` holds the same content (matched by
 * key), so memoized rows skip rendering for every item an incoming event did not touch. `groupEvents`
 * rebuilds every item, and its leaves (text, tool input) keep the identity of the event they came from,
 * so unchanged items compare equal in a few reference checks.
 */
export function shareItems(previous: FeedItem[], next: FeedItem[]): FeedItem[] {
  if (!previous.length) return next;
  const before = new Map<string, FeedItem>();
  for (const item of previous) before.set(item.key, item);
  for (let i = 0; i < next.length; i++) {
    const prior = before.get(next[i]!.key);
    if (prior && sameValue(prior, next[i])) next[i] = prior;
  }
  // An event that changes nothing visible (usage, a repeated announcement) keeps the whole list.
  return next.length === previous.length && next.every((item, i) => item === previous[i]) ? previous : next;
}

const sameList = (a: readonly FeedItem[], b: readonly FeedItem[]) => a.length === b.length && a.every((item, i) => item === b[i]);

/**
 * Reuses the entry objects of `previous` wherever the regrouped `next` holds the same items (matched by key), so memoized
 * feed rows skip rendering for every turn an incoming event did not touch. Items already keep their identity through `shareItems`.
 */
export function shareEntries(previous: readonly TurnEntry[], next: TurnEntry[]): TurnEntry[] {
  if (!previous.length) return next;
  const before = new Map<string, TurnEntry>();
  for (const entry of previous) before.set(entry.key, entry);
  for (let i = 0; i < next.length; i++) {
    const entry = next[i]!, prior = before.get(entry.key);
    if (!prior || prior.at !== entry.at) continue;
    const same = prior.type === "item"
      ? entry.type === "item" && prior.item === entry.item
      : entry.type === "run" && prior.status === entry.status && prior.durationMs === entry.durationMs && sameList(prior.steps, entry.steps) && sameList(prior.attention, entry.attention) && sameList(prior.answer, entry.answer);
    if (same) next[i] = prior;
  }
  return next.length === previous.length && next.every((entry, i) => entry === previous[i]) ? (previous as TurnEntry[]) : next;
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

// ---- assistant turns ----

/** The label of a reasoning/commentary block: its `**title**`, else its first line, without markdown markers. */
export function reasoningTitle(text: string, fallback = "Thinking"): string {
  const bold = /\*\*([^*\n]+?)\*\*/.exec(text)?.[1] ?? "";
  const line = text.split(/\r?\n/).map((l) => l.replace(/^[\s>#*_\-`]+/, "").replace(/\*\*|__|`/g, "").trim()).find(Boolean) ?? "";
  const title = clip((bold || line).replace(/\*\*|__|`/g, ""), 80);
  return title || fallback;
}

export type RunState = "running" | "requires-action" | "complete" | "cancelled" | "error" | "incomplete";

/**
 * One assistant turn, from a user message to the next: `steps` (reasoning, tool calls, messages written while working...) form
 * the run's tree, open while the turn is in flight and folded once it settles; `attention` (pending approvals and questions,
 * errors, notices) and `answer` (the closing message of a settled turn and any files or images) always stay visible, each in
 * original order.
 */
export interface TurnRun {
  type: "run";
  key: string;
  at: string;
  status: RunState;
  /** When the turn began (its user message, else its first event) in Unix ms; null when the timestamp does not parse. */
  startedAt: number | null;
  /** First to last event of the turn in ms; null when unknown or under a second. */
  durationMs: number | null;
  steps: FeedItem[];
  attention: FeedItem[];
  answer: FeedItem[];
}
export type TurnEntry = { type: "item"; key: string; at: string; item: FeedItem } | TurnRun;

export interface RunContext {
  /** The session is working right now: the last turn is in flight. */
  running: boolean;
  /** The agent process is alive: a turn that never reported completion is `incomplete` once it is not. */
  live: boolean;
  pendingPermissions: ReadonlySet<string>;
  pendingQuestions: ReadonlySet<string>;
}

const CANCELLED = /\b(cancel+ed|interrupted|aborted|stopped)\b/i;

function closeTurn(entries: TurnEntry[], user: Extract<FeedItem, { type: "user" }> | null, buffer: FeedItem[], last: boolean, ctx: RunContext) {
  if (!buffer.length) {
    // A prompt the agent has not answered yet still shows that it is working.
    if (user && last && ctx.running) entries.push({ type: "run", key: `run:${user.key}`, at: user.at, status: "running", startedAt: msOf(user.at) ?? null, durationMs: null, steps: [], attention: [], answer: [] });
    return;
  }
  const steps: FeedItem[] = [], attention: FeedItem[] = [], media: FeedItem[] = [];
  const messages: { item: FeedItem; at: number }[] = [];
  let turn: Extract<FeedItem, { type: "turn" }> | null = null;
  let pending = false;
  let lastStep = -1;
  for (const [index, item] of buffer.entries()) {
    switch (item.type) {
      case "assistant": messages.push({ item, at: index }); break;
      case "reasoning": case "tool": case "sources": case "subagent": steps.push(item); lastStep = index; break;
      case "note":
        if (item.kind === "needs_input") attention.push(item); else { steps.push(item); lastStep = index; }
        break;
      case "permission":
        if (!item.decision && ctx.pendingPermissions.has(item.request.id)) { attention.push(item); pending = true; } else { steps.push(item); lastStep = index; }
        break;
      case "question":
        if (!item.answers && !item.cancelled && ctx.pendingQuestions.has(item.request.id)) { attention.push(item); pending = true; } else { steps.push(item); lastStep = index; }
        break;
      case "guardrail": case "error": attention.push(item); break;
      case "image": case "file": case "generation": media.push(item); break;
      case "turn": turn = item; break;
      case "user": break;
    }
  }
  let status: RunState;
  if (pending) status = "requires-action";
  else if (turn) status = turn.ok ? "complete" : CANCELLED.test(turn.result) ? "cancelled" : "error";
  else if (last && ctx.running) status = "running";
  else if (last && !ctx.live) status = "incomplete";
  else if (attention.some((item) => item.type === "error")) status = "error";
  else status = "complete";
  if (turn && !turn.ok && status !== "requires-action") attention.push(turn);

  // While the turn is in flight every message is work in progress, shown live inside the run; once it settles, the messages
  // after its last activity become the answer.
  const inFlight = status === "running" || status === "requires-action";
  const answer: FeedItem[] = [...media];
  for (const message of messages) {
    if (!inFlight && message.at > lastStep) answer.push(message.item); else steps.push(message.item);
  }
  const order = new Map(buffer.map((item, index) => [item, index]));
  const byOrder = (a: FeedItem, b: FeedItem) => order.get(a)! - order.get(b)!;
  steps.sort(byOrder); answer.sort(byOrder);

  const first = Date.parse((user ?? buffer[0]!).at), end = Date.parse(buffer[buffer.length - 1]!.at);
  const span = end - first;
  const durationMs = status !== "running" && Number.isFinite(span) && span >= 1000 ? span : null;
  const at = buffer[0]!.at;
  if (!steps.length && !attention.length && status === "complete") {
    for (const item of answer) entries.push({ type: "item", key: item.key, at: item.at, item });
    return;
  }
  entries.push({ type: "run", key: `run:${user?.key ?? buffer[0]!.key}`, at, status, startedAt: Number.isFinite(first) ? first : null, durationMs, steps, attention, answer });
}

/**
 * Groups feed items into one entry per user message and one run per assistant turn. Once a turn settles, its closing assistant
 * message(s), meaning those after its last piece of activity, are its answer; every other message is a step inside the run.
 */
export function groupRuns(items: readonly FeedItem[], ctx: RunContext): TurnEntry[] {
  const entries: TurnEntry[] = [];
  let user: Extract<FeedItem, { type: "user" }> | null = null;
  let buffer: FeedItem[] = [];
  for (const item of items) {
    if (item.type !== "user") { buffer.push(item); continue; }
    closeTurn(entries, user, buffer, false, ctx);
    entries.push({ type: "item", key: item.key, at: item.at, item });
    user = item;
    buffer = [];
  }
  closeTurn(entries, user, buffer, true, ctx);
  return entries;
}
