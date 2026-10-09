import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SCENES } from "./scenes";

const bridge = vi.hoisted(() => ({
  all: vi.fn(),
  set: vi.fn(),
  stored: {} as Record<string, unknown>,
  changed: undefined as ((event: { key: string; value: unknown }) => void) | undefined,
}));
vi.mock("@/lib/backend", () => ({ inDesktop: true, previewKey: "ascii-startup-test" }));
vi.mock("@/mygo", () => ({
  Settings: { all: bridge.all, set: bridge.set },
  events: { settingsChanged: { on: (handler: typeof bridge.changed) => { bridge.changed = handler; } } },
}));

// Dynamic imports are intentional: each test resets the renderer-local startup
// singleton, and the restart test must exercise a fresh module-loading boundary.
beforeEach(() => {
  vi.resetModules();
  vi.clearAllMocks();
  bridge.stored = { ascii: { scene: "night-coast", randomOnStartup: true } };
  bridge.changed = undefined;
  bridge.all.mockImplementation(async () => structuredClone(bridge.stored));
  bridge.set.mockImplementation(async (key: string, value: unknown) => { bridge.stored[key] = structuredClone(value); });
});
afterEach(() => { vi.restoreAllMocks(); });

describe("ASCII startup scene", () => {
  it("waits for persisted preferences and samples once across concurrent startup loads", async () => {
    const random = vi.spyOn(Math, "random").mockReturnValue(0.5);
    let finish!: (value: Record<string, unknown>) => void;
    bridge.all.mockReturnValue(new Promise<Record<string, unknown>>((resolve) => { finish = resolve; }));
    const { useSettings } = await import("@/stores/settings");
    const first = useSettings.getState().load();
    const second = useSettings.getState().load();
    expect(random).not.toHaveBeenCalled();
    expect(useSettings.getState().loaded).toBe(false);
    finish(bridge.stored);
    await Promise.all([first, second]);
    expect(random).toHaveBeenCalledTimes(1);
    expect(useSettings.getState().activeAsciiScene).toBe(SCENES[Math.floor(SCENES.length / 2)]!.id);
    expect(useSettings.getState().ascii.scene).toBe("night-coast");
    expect(bridge.set).not.toHaveBeenCalled();
  });

  it("keeps the startup choice through subscriber remounts, focus, reloads and settings events", async () => {
    const random = vi.spyOn(Math, "random").mockReturnValue(0.8);
    const { useSettings } = await import("@/stores/settings");
    await useSettings.getState().load();
    const scene = useSettings.getState().activeAsciiScene;
    const unmount = useSettings.subscribe(() => {});
    unmount();
    const remount = useSettings.subscribe(() => {});
    window.dispatchEvent(new Event("blur"));
    window.dispatchEvent(new Event("focus"));
    useSettings.getState().set("appearance", { density: "comfortable" });
    useSettings.getState().set("ascii", { opacity: 0.5, paused: true, staticOnly: true });
    bridge.changed!({ key: "ascii", value: bridge.stored.ascii });
    await useSettings.getState().load();
    remount();
    expect(useSettings.getState().activeAsciiScene).toBe(scene);
    expect(random).toHaveBeenCalledTimes(1);
    expect(bridge.set).toHaveBeenCalledWith("ascii", expect.objectContaining({
      scene: "night-coast", randomOnStartup: true, opacity: 0.5, paused: true, staticOnly: true,
    }));
  });

  it("manual selection disables random even when selecting the current random scene", async () => {
    const random = vi.spyOn(Math, "random").mockReturnValue(0.9);
    const { useSettings } = await import("@/stores/settings");
    await useSettings.getState().load();
    const scene = useSettings.getState().activeAsciiScene;
    useSettings.getState().set("ascii", { scene });
    expect(useSettings.getState().ascii).toMatchObject({ scene, randomOnStartup: false });
    expect(bridge.set).toHaveBeenLastCalledWith("ascii", expect.objectContaining({ scene, randomOnStartup: false }));
    bridge.changed!({ key: "ascii", value: bridge.stored.ascii });
    await useSettings.getState().load();
    expect(useSettings.getState().activeAsciiScene).toBe(scene);
    expect(random).toHaveBeenCalledTimes(1);
  });

  it("preserves existing manual preferences and defers newly enabled random mode until restart", async () => {
    bridge.stored = { ascii: { scene: "kyoto-dusk", paused: true, staticOnly: true, respectReducedMotion: true } };
    const random = vi.spyOn(Math, "random").mockReturnValue(0.1);
    const { useSettings } = await import("@/stores/settings");
    await useSettings.getState().load();
    expect(useSettings.getState().ascii.randomOnStartup).toBe(false);
    useSettings.getState().set("ascii", { randomOnStartup: true });
    await useSettings.getState().load();
    expect(useSettings.getState().activeAsciiScene).toBe("kyoto-dusk");
    expect(random).not.toHaveBeenCalled();
    expect(bridge.stored.ascii).toMatchObject({
      scene: "kyoto-dusk", randomOnStartup: true, paused: true, staticOnly: true, respectReducedMotion: true,
    });
    // A new module instance represents a new renderer startup, not a Home remount.
    vi.resetModules();
    const restarted = (await import("@/stores/settings")).useSettings;
    await restarted.getState().load();
    expect(restarted.getState().activeAsciiScene).toBe(SCENES[Math.floor(0.1 * SCENES.length)]!.id);
    expect(random).toHaveBeenCalledTimes(1);
  });

  it("keeps uploads through restart and opacity changes, but replaces them on scene selection", async () => {
    const { useSettings } = await import("@/stores/settings");
    await useSettings.getState().load();
    const customBackground = { name: "coast.png", pixels: btoa("\x55\xaa\xff".repeat(200 * 100)) };
    useSettings.getState().set("ascii", { customBackground });
    expect(useSettings.getState().ascii.randomOnStartup).toBe(false);
    useSettings.getState().set("ascii", { opacity: 0.5 });
    vi.resetModules();
    const restarted = (await import("@/stores/settings")).useSettings;
    await restarted.getState().load();
    expect(restarted.getState().ascii).toMatchObject({ customBackground, opacity: 0.5, randomOnStartup: false });
    restarted.getState().set("ascii", { scene: "kyoto-dusk" });
    expect(restarted.getState().ascii.customBackground).toBeNull();
    expect(restarted.getState().activeAsciiScene).toBe("kyoto-dusk");
    restarted.getState().set("ascii", { customBackground });
    restarted.getState().set("ascii", { randomOnStartup: true });
    expect(restarted.getState().ascii.customBackground).toBeNull();
  });

  it("waits for a successful settings read before choosing after a failed startup read", async () => {
    const random = vi.spyOn(Math, "random").mockReturnValue(0.3);
    bridge.all.mockRejectedValueOnce(new Error("daemon unavailable"));
    const { useSettings } = await import("@/stores/settings");
    await useSettings.getState().load();
    expect(random).not.toHaveBeenCalled();
    expect(useSettings.getState().error).toBe("daemon unavailable");
    await useSettings.getState().load();
    expect(random).toHaveBeenCalledTimes(1);
    expect(useSettings.getState().error).toBeNull();
  });
});
