import { describe, expect, it } from "vitest";
import type { Message, MsgPart } from "@/mygo";
import { convertMessage, convertStatus, SOURCE_KEY } from "./convert";

const message = (parts: MsgPart[], over: Partial<Message> = {}): Message => ({
  id: "m1", botId: "a", seq: 2, role: "assistant", author: "bot:a", parts, status: "complete", createdAt: 1000, ...over,
});
const content = (m: Message) => {
  const converted = convertMessage(m);
  return typeof converted.content === "string" ? [] : converted.content;
};

describe("convertMessage", () => {
  it("maps text to native parts, drops empty ones and never shows the model's reasoning", () => {
    expect(content(message([{ type: "text", text: "Hi" }, { type: "reasoning", text: "Hmm" }, { type: "text", text: "" }]))).toEqual([
      { type: "text", text: "Hi" },
    ]);
  });

  it("maps a finished tool row to a tool-call with a result, a running one without, a failed one as an error", () => {
    const [done, running, failed] = content(message([
      { type: "tool", id: "t1", name: "web_search", status: "done", summary: "3 results" },
      { type: "tool", id: "t2", name: "web_fetch", status: "running" },
      { type: "tool", id: "t3", name: "web_fetch", status: "error", summary: "timeout" },
    ]));
    expect(done).toMatchObject({ type: "tool-call", toolCallId: "t1", toolName: "web_search", args: {}, result: "3 results" });
    expect(running).toMatchObject({ type: "tool-call", toolCallId: "t2", args: {} });
    expect(running).not.toHaveProperty("result");
    expect(failed).toMatchObject({ isError: true, result: "timeout" });
  });

  it("maps citations to source parts and skips ones without a url", () => {
    expect(content(message([{ type: "citation", url: "https://a.test/x", title: "A" }, { type: "citation", title: "no url" }]))).toEqual([
      { type: "source", sourceType: "url", id: "https://a.test/x", url: "https://a.test/x", title: "A" },
    ]);
  });

  it("maps report, delegation, memory and notice to named data parts", () => {
    const parts: MsgPart[] = [
      { type: "report", title: "R", text: "# R" }, { type: "delegation", name: "Coder", status: "done" },
      { type: "memory", text: "likes tea" }, { type: "notice", text: "Compacted" },
    ];
    expect(content(message(parts)).map((p) => p.type)).toEqual(["data-report", "data-delegation", "data-memory", "data-notice"]);
    expect(content(message(parts))[0]).toMatchObject({ data: parts[0] });
  });

  it("maps user text and attachments, with attachments as completed chips", () => {
    const converted = convertMessage(message([
      { type: "attachment", attachmentId: "f1", name: "a.png", mime: "image/png", size: 5 },
      { type: "attachment", attachmentId: "f2", name: "n.txt", mime: "text/plain" },
      { type: "text", text: "look" },
    ], { role: "user", author: "user" }));
    expect(converted.role).toBe("user");
    expect(converted.content).toEqual([{ type: "text", text: "look" }]);
    expect(converted.attachments).toMatchObject([
      { id: "f1", type: "image", name: "a.png", contentType: "image/png", status: { type: "complete" } },
      { id: "f2", type: "file", name: "n.txt" },
    ]);
    expect(converted).not.toHaveProperty("status");
  });

  it("shows a system message as the bot's and keeps the daemon record for rendering", () => {
    const source = message([{ type: "notice", text: "x" }], { role: "system" });
    const converted = convertMessage(source);
    expect(converted.role).toBe("assistant");
    expect(converted.metadata?.custom?.[SOURCE_KEY]).toBe(source);
    expect(converted.createdAt).toEqual(new Date(1000));
  });

  it("returns the same object for the same snapshot", () => {
    const source = message([{ type: "text", text: "x" }]);
    expect(convertMessage(source)).toBe(convertMessage(source));
  });
});

describe("convertStatus", () => {
  it("maps every daemon status", () => {
    expect(convertStatus({ status: "running" })).toEqual({ type: "running" });
    expect(convertStatus({ status: "pending" })).toEqual({ type: "running" });
    expect(convertStatus({ status: "complete" })).toEqual({ type: "complete", reason: "stop" });
    expect(convertStatus({ status: "error", error: "bad key" })).toEqual({ type: "incomplete", reason: "error", error: "bad key" });
    expect(convertStatus({ status: "canceled" })).toEqual({ type: "incomplete", reason: "cancelled" });
    expect(convertStatus({ status: "incomplete" })).toEqual({ type: "incomplete", reason: "other" });
  });
});
