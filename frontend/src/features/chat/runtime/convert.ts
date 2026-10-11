import type { ThreadMessageLike } from "@assistant-ui/react";
import type { Message, MsgPart } from "@/mygo";

/** What assistant-ui keeps in `metadata.custom` for a converted message: the daemon's own record, which the renderer draws from. */
export const SOURCE_KEY = "message";

/** Daemon part types that assistant-ui has no native part for; they travel as named `data` parts. */
const DATA_PARTS: Record<string, true> = { report: true, delegation: true, memory: true, notice: true, attachment: true };

type Content = Exclude<ThreadMessageLike["content"], string>;
type Part = Content[number];

function assistantPart(part: MsgPart, index: number): Part | null {
  switch (part.type) {
    case "text":
      return part.text ? { type: "text", text: part.text } : null;
    case "tool": {
      // The tool UI fallback renders a tool-call part; args are not part of the daemon's row, so they stay empty.
      const done = (part.status ?? "").toLowerCase() === "done";
      return {
        type: "tool-call",
        toolCallId: part.id || `tool-${index}`,
        toolName: part.name || "tool",
        args: {},
        ...(done ? { result: part.summary || "done" } : {}),
        ...((part.status ?? "").toLowerCase() === "error" ? { isError: true, result: part.summary || "error" } : {}),
      };
    }
    case "citation":
      return part.url ? { type: "source", sourceType: "url", id: part.url, url: part.url, ...(part.title ? { title: part.title } : {}) } : null;
    default:
      return DATA_PARTS[part.type] ? { type: `data-${part.type}`, data: part } : null;
  }
}

/** assistant-ui's status for a daemon message status; only assistant messages carry one. */
export function convertStatus(message: Pick<Message, "status" | "error">): NonNullable<ThreadMessageLike["status"]> {
  switch (message.status) {
    case "running":
    case "pending":
      return { type: "running" };
    case "error":
      return { type: "incomplete", reason: "error", error: message.error || "The reply failed." };
    case "canceled":
      return { type: "incomplete", reason: "cancelled" };
    case "incomplete":
      return { type: "incomplete", reason: "other" };
    default:
      return { type: "complete", reason: "stop" };
  }
}

const cache = new WeakMap<Message, ThreadMessageLike>();

/**
 * A daemon message as assistant-ui's ThreadMessageLike: text, reasoning and tool rows map to the native parts,
 * citations to `source` parts, user attachments to attachments, and report/delegation/memory/notice to `data-*`
 * parts rendered by name. The original rides along in `metadata.custom` so rendering never loses a field.
 * A "system" message is shown as the bot's (the daemon only uses it for engine output).
 */
export function convertMessage(message: Message): ThreadMessageLike {
  const cached = cache.get(message);
  if (cached) return cached;
  const base = { id: message.id, createdAt: new Date(message.createdAt), metadata: { custom: { [SOURCE_KEY]: message } } };
  let converted: ThreadMessageLike;
  if (message.role === "user") {
    converted = {
      ...base,
      role: "user",
      content: message.parts.filter((p) => p.type === "text" && p.text && !p.hidden).map((p): Part => ({ type: "text", text: p.text! })),
      attachments: message.parts.filter((p) => p.type === "attachment").map((p, i) => ({
        id: p.attachmentId || `attachment-${i}`,
        type: p.mime?.startsWith("image/") ? "image" : "file",
        name: p.name || p.title || "Attachment",
        ...(p.mime ? { contentType: p.mime } : {}),
        status: { type: "complete" as const },
        content: [],
      })),
    };
  } else {
    converted = {
      ...base,
      role: "assistant",
      content: message.parts.map(assistantPart).filter((p): p is Part => p !== null),
      status: convertStatus(message),
    };
  }
  cache.set(message, converted);
  return converted;
}
