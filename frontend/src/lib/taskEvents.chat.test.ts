import { describe, expect, it } from "vitest";
import type { Event as AgentEvent } from "@/mygo";
import { FEED_KINDS, groupEvents, mergeEvent, mergeHistory } from "./taskEvents";

const event = (id: number, kind: string, data: unknown, text = ""): AgentEvent => ({ id, kind, data, text, at: `2026-10-08T12:00:${String(id).padStart(2, "0")}Z`, sessionId: "s" });
const request = { id: "q", questions: [{ id: "choice", prompt: "Which?", options: [{ id: "a", label: "A" }], multiple: false, allowText: false }] };

describe("chat event correlations", () => {
  it.each([false, true])("folds an image and generation updates regardless of arrival order (image first: %s)", (imageFirst) => {
    const image = { id: "g", path: "out.png", mime: "image/png", prompt: "Fox" };
    const events = imageFirst ? [event(1, "image", image), event(2, "image_status", { id: "g", status: "complete" })] : [event(1, "image_status", { id: "g", status: "running" }), event(2, "image", image)];
    events.push(event(3, "image_status", { id: "g", status: "complete", prompt: "Fox" }), event(4, "image", image));
    const items = groupEvents(events);
    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({ type: "generation", key: "1", at: events[0]!.at, generation: { status: "complete" }, image: { path: "out.png" } });
  });

  it("does not guess correlations between unrelated generation and image IDs", () => {
    expect(groupEvents([event(1, "image_status", { id: "a", status: "running" }), event(2, "image", { id: "b", path: "b.png" })]).map((item) => item.type)).toEqual(["generation", "image"]);
  });

  it("folds resolved permissions without leaving duplicate decision notes", () => {
    const items = groupEvents([event(1, "permission", { id: "p", tool: "write" }), event(2, "message", null, "Continue"), event(3, "decision", { id: "p", allow: false, reason: "Not approved" })]);
    expect(items.map((item) => item.type)).toEqual(["permission", "assistant"]);
    expect(items[0]).toMatchObject({ decision: { allow: false, reason: "Not approved" } });
    expect(groupEvents([event(3, "decision", { id: "p", allow: false }, "Denied")])[0]).toMatchObject({ type: "note", text: "Denied" });
  });

  it("folds question answers and cancellations while retaining the original position", () => {
    const events = [event(1, "question", request), event(2, "message", null, "Waiting"), event(3, "question", request), event(4, "answer", { id: "q", answers: [{ questionId: "choice", values: ["a"] }] })];
    const answered = groupEvents(events);
    expect(answered).toHaveLength(2);
    expect(answered[0]).toMatchObject({ key: "1", at: events[0]!.at, type: "question", answers: [{ questionId: "choice", values: ["a"] }], cancelled: false });
    expect(groupEvents([...events, event(5, "question_cancel", { id: "q" })])[0]).toMatchObject({ cancelled: true });
  });

  it("replaces same-id subagents and files without inventing completion", () => {
    const events = [event(1, "subagent", { id: "child", name: "Research", status: "running" }), event(2, "file", { id: "f", path: "a.ts", status: "created" }), event(3, "subagent", { id: "child", name: "Research", status: "waiting", message: "Need input" }), event(4, "file", { id: "f", path: "a.ts", status: "modified" }), event(5, "turn_done", { ok: true })];
    const items = groupEvents(events, { live: false });
    expect(items.map((item) => item.type)).toEqual(["subagent", "file", "turn"]);
    expect(items[0]).toMatchObject({ key: "1", at: events[0]!.at, agent: { status: "waiting", message: "Need input" } });
    expect(items[1]).toMatchObject({ key: "2", file: { status: "modified" } });
  });

  it("separates actual guardrails from generic errors and rejects malformed rich payloads", () => {
    const items = groupEvents([event(1, "error", null, "Timeout"), event(2, "guardrail", { id: "r", kind: "refusal", message: "Policy stop", source: "provider" }), event(3, "sources", { items: [null, { url: "https://example.test", title: "Reference" }] }), event(4, "question", { id: "q", questions: [42, null] }), event(5, "image_status", { id: "x", status: "imaginary" }), event(6, "subagent", {})]);
    expect(items.map((item) => item.type)).toEqual(["error", "guardrail", "sources"]);
  });

  it("retains all shared kinds through live/history merging without duplicating IDs", () => {
    const kinds = ["usage", "question", "question_cancel", "answer", "sources", "file", "image_status", "subagent", "guardrail"];
    const history = kinds.map((kind, i) => event(i + 1, kind, {}));
    expect(kinds.every((kind) => FEED_KINDS.includes(kind))).toBe(true);
    expect(mergeHistory(history, history)).toEqual(history);
    expect(history.reduce(mergeEvent, [] as AgentEvent[])).toEqual(history);
  });
});
