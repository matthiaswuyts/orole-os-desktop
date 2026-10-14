import { describe, expect, it } from "vitest";
import { initialForm } from "@/features/chat/builder/botForm";
import { jarvisGate } from "./gate";

const full = { ...initialForm(), codeTasks: true, toolAccess: "all", taskPermissionMode: "yolo" };
const open = { botYolo: true };

describe("jarvisGate", () => {
  it("passes with full permissions", () => {
    expect(jarvisGate(full, open)).toBe("");
  });

  it("reports the first failing condition in the daemon's order", () => {
    expect(jarvisGate({ ...full, toolAccess: "write", codeTasks: false }, open)).toBe("Jarvis needs this bot's tool access set to All.");
    expect(jarvisGate({ ...full, codeTasks: false, taskPermissionMode: "auto" }, open)).toBe("Jarvis needs code tasks turned on for this bot.");
    expect(jarvisGate({ ...full, taskPermissionMode: "auto" }, open)).toBe("Jarvis needs this bot's code tasks set to YOLO.");
    expect(jarvisGate(full, { botYolo: true, yoloDisabled: true })).toBe("Jarvis needs YOLO mode allowed in Task permissions.");
    expect(jarvisGate(full, {})).toBe("Jarvis needs \"Bots may start YOLO tasks\" turned on in Bot code tasks.");
  });

  it("falls back to the global settings for unset overrides", () => {
    const inherit = { ...full, toolAccess: "", taskPermissionMode: "" };
    expect(jarvisGate(inherit, { ...open, botToolAccess: "all", botTasks: { permissionMode: "yolo" } })).toBe("");
    expect(jarvisGate(inherit, open)).toBe("Jarvis needs this bot's tool access set to All.");
    expect(jarvisGate(inherit, { ...open, botToolAccess: "all" })).toBe("Jarvis needs this bot's code tasks set to YOLO.");
  });

  it("judges only the bot's own values while the global settings are still loading", () => {
    expect(jarvisGate({ ...full, toolAccess: "" }, null)).toBe("Jarvis needs this bot's tool access set to All.");
    expect(jarvisGate(full, null)).toBe("Jarvis needs \"Bots may start YOLO tasks\" turned on in Bot code tasks.");
  });
});
