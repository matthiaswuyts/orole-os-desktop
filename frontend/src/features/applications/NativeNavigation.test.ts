import { beforeEach, describe, expect, it, vi } from "vitest";
import type { DesktopNavigation } from "@/mygo";

const bridge = vi.hoisted(() => ({
  ready: vi.fn(),
  off: vi.fn(),
  setView: vi.fn(),
  openSettings: vi.fn(),
  listener: null as ((request: DesktopNavigation) => void) | null,
}));
vi.mock("@/mygo", () => ({
  Desktop: { ready: bridge.ready },
  events: { desktopNavigate: { on: (listener: (request: DesktopNavigation) => void) => { bridge.listener = listener; return bridge.off; } } },
}));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/features/registry", () => ({ views: { home: true, settings: true } }));
vi.mock("@/stores/ui", () => ({ useUI: { getState: () => ({ setView: bridge.setView, openSettings: bridge.openSettings }) } }));
import { connectNativeNavigation } from "./NativeNavigation";

beforeEach(() => { vi.clearAllMocks(); bridge.listener = null; });

describe("native navigation lifecycle", () => {
  it.each(["skills", "mcp"])("opens %s in Settings once on cold start", async (section) => {
    bridge.ready.mockResolvedValue({ view: `settings/${section}`, sequence: 1 });
    const off = connectNativeNavigation();
    await Promise.resolve();
    bridge.listener?.({ view: `settings/${section}`, sequence: 1 });
    expect(bridge.openSettings).toHaveBeenCalledExactlyOnceWith(section);
    expect(bridge.setView).not.toHaveBeenCalled();
    off();
  });

  it("never overwrites a newer event with an older initial snapshot", async () => {
    let finish!: (request: DesktopNavigation) => void;
    bridge.ready.mockImplementation(() => new Promise<DesktopNavigation>((resolve) => { finish = resolve; }));
    const off = connectNativeNavigation();
    bridge.listener?.({ view: "settings", sequence: 2 });
    finish({ view: "home", sequence: 1 });
    await Promise.resolve();
    expect(bridge.setView).toHaveBeenCalledExactlyOnceWith("settings");
    off();
  });

  it("rejects non-module targets and ignores completion after unmount", async () => {
    let finish!: (request: DesktopNavigation) => void;
    bridge.ready.mockImplementation(() => new Promise<DesktopNavigation>((resolve) => { finish = resolve; }));
    const off = connectNativeNavigation();
    for (const view of ["skills", "mcp", "settings/unknown", "https://example.com", "machines", "files", "terminals", "applications", "C:\\app.exe", "launch", "__proto__"]) bridge.listener?.({ view, sequence: 1 });
    expect(bridge.setView).not.toHaveBeenCalled();
    off();
    finish({ view: "home", sequence: 1 });
    await Promise.resolve();
    bridge.listener?.({ view: "settings/skills", sequence: 2 });
    expect(bridge.setView).not.toHaveBeenCalled();
    expect(bridge.openSettings).not.toHaveBeenCalled();
    expect(bridge.off).toHaveBeenCalledOnce();
  });
});
