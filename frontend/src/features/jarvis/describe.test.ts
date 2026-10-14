import { describe, expect, it } from "vitest";
import type { JarvisRule } from "@/mygo";
import { describeRule } from "./describe";

const rule = (over: Partial<JarvisRule>): JarvisRule => ({ id: "r", botId: "b", trigger: "task_failed", target: "any", repeat: true, urgent: false, quietOverride: false, note: "", createdAt: 1, ...over });

describe("describeRule", () => {
  it.each<[string, Partial<JarvisRule>, string]>([
    ["any task finishes", { trigger: "task_finished" }, "When any task I start finishes"],
    ["any task fails", { trigger: "task_failed" }, "When any task I start fails"],
    ["one task", { trigger: "task_finished", target: "0123456789abcdef" }, "When task 01234567 finishes"],
    ["any automation succeeds", { trigger: "automation_finished" }, "When any automation succeeds"],
    ["one automation fails", { trigger: "automation_failed", target: "auto-1234-xyz" }, "When automation auto-123 fails"],
    ["notification kind", { trigger: "notification", target: "mail" }, "On mail notifications"],
    ["notification kind and source", { trigger: "notification", target: "mail:Gmail" }, "On mail notifications from Gmail"],
    ["daily time", { trigger: "at_time", target: "09:30" }, "Every day at 09:30"],
    ["one-off time", { trigger: "at_time", target: "09:30", repeat: false }, "At 09:30 · once"],
    ["suffixes", { trigger: "task_failed", repeat: false, quietOverride: true, urgent: true }, "When any task I start fails · once · even in quiet hours · urgent"],
  ])("%s", (_name, over, text) => {
    expect(describeRule(rule(over))).toBe(text);
  });

  it("adds an expiry date", () => {
    const text = describeRule(rule({ expiresAt: Date.UTC(2026, 11, 24, 12) }));
    expect(text).toMatch(/^When any task I start fails · until .*2026/);
  });
});
