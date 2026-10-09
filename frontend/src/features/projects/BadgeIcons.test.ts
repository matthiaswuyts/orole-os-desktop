import { describe, expect, it } from "vitest";
import type { Machine, MachineHealth } from "@/mygo";
import { hostFamily, osFamily } from "./BadgeIcons";

describe("osFamily", () => {
  it.each([
    ["windows", "windows"], ["Windows 11 Pro", "windows"], ["win32", "windows"],
    ["darwin", "apple"], ["macOS 15", "apple"],
    ["linux", "linux"], ["Ubuntu 24.04", "linux"],
    ["freebsd", null], ["", null],
    ["Mozilla/5.0 (Windows NT 10.0; Win64; x64)", "windows"],
    ["Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)", "apple"],
    ["Mozilla/5.0 (X11; Linux x86_64)", "linux"],
  ])("%s -> %s", (os, family) => expect(osFamily(os)).toBe(family));
});

describe("hostFamily", () => {
  const machine = (os: string) => ({ os }) as Machine;
  const health = (os: string) => ({ os }) as MachineHealth;
  it("prefers a live health report, then the registered machine", () => {
    expect(hostFamily("box", machine("linux"), health("darwin"))).toBe("apple");
    expect(hostFamily("box", machine("windows"), null)).toBe("windows");
    expect(hostFamily("box", machine("windows"), health(""))).toBe("windows");
  });
  it("shows nothing for an unknown remote host, but falls back to this browser's platform for the local system", () => {
    expect(hostFamily("box", undefined, null)).toBeNull();
    expect(hostFamily("local", undefined, null)).toBe(osFamily(navigator.userAgent));
  });
});
