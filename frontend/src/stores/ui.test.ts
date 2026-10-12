import { beforeEach, describe, expect, it, vi } from "vitest";

const fresh = async () => {
  vi.resetModules();
  return (await import("./ui")).useUI;
};

beforeEach(() => localStorage.clear());

describe("ui navigation", () => {
  it("starts on Home, whatever an earlier two-mode sidebar left in storage", async () => {
    localStorage.setItem("orole.mode", "chat");
    const useUI = await fresh();
    expect(useUI.getState().view).toBe("home");
  });

  it("focusAutomation opens the Automations view with the flow handed off", async () => {
    const useUI = await fresh();
    useUI.getState().focusAutomation("nightly");
    expect(useUI.getState()).toMatchObject({ view: "automations", automationFocus: "nightly" });
    useUI.getState().focusAutomation(null);
    expect(useUI.getState()).toMatchObject({ view: "automations", automationFocus: null });
  });
});

describe("opening settings", () => {
  it("opens the settings window in the desktop app and leaves the current view alone", async () => {
    vi.resetModules();
    const openSettings = vi.fn().mockResolvedValue(undefined);
    vi.doMock("@/lib/backend", () => ({ inDesktop: true }));
    vi.doMock("@/mygo", () => ({ Desktop: { openSettings } }));
    const { useUI } = await import("./ui");
    useUI.getState().setView("review");
    useUI.getState().openSettings("integrations");
    expect(openSettings).toHaveBeenCalledWith("integrations");
    expect(useUI.getState()).toMatchObject({ view: "review", settingsSection: "integrations" });
    vi.doUnmock("@/lib/backend");
    vi.doUnmock("@/mygo");
  });

  it("shows settings in place in the browser preview", async () => {
    vi.resetModules();
    vi.doMock("@/lib/backend", () => ({ inDesktop: false }));
    const { useUI } = await import("./ui");
    useUI.getState().openSettings("ai");
    expect(useUI.getState()).toMatchObject({ view: "settings", settingsSection: "ai" });
    vi.doUnmock("@/lib/backend");
  });
});
