import { beforeEach, describe, expect, it, vi } from "vitest";

// The persisted mode is read when the store module is created, so each case re-evaluates the module (a module-loading boundary a static import cannot exercise).
const fresh = async () => {
  vi.resetModules();
  return (await import("./ui")).useUI;
};

beforeEach(() => localStorage.clear());

describe("ui mode", () => {
  it("starts in Code on the home view by default", async () => {
    const useUI = await fresh();
    expect(useUI.getState()).toMatchObject({ mode: "code", view: "home", lastCodeView: "home" });
  });

  it("switching to Chat shows the chat view and persists; switching back restores the last Code view", async () => {
    const useUI = await fresh();
    useUI.getState().setView("issues");
    useUI.getState().setMode("chat");
    expect(useUI.getState()).toMatchObject({ mode: "chat", view: "chat" });
    expect(localStorage.getItem("orole.mode")).toBe("chat");
    useUI.getState().setMode("code");
    expect(useUI.getState()).toMatchObject({ mode: "code", view: "issues" });
    expect(localStorage.getItem("orole.mode")).toBe("code");
  });

  it("restores a persisted Chat mode on startup", async () => {
    localStorage.setItem("orole.mode", "chat");
    const useUI = await fresh();
    expect(useUI.getState()).toMatchObject({ mode: "chat", view: "chat" });
    useUI.getState().setMode("code");
    expect(useUI.getState().view).toBe("home");
  });

  it("ignores an unknown persisted value", async () => {
    localStorage.setItem("orole.mode", "{not json");
    const useUI = await fresh();
    expect(useUI.getState().mode).toBe("code");
  });

  it("survives unavailable storage", async () => {
    const get = vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("denied"); });
    const set = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("denied"); });
    const useUI = await fresh();
    expect(useUI.getState().mode).toBe("code");
    useUI.getState().setMode("chat");
    expect(useUI.getState().view).toBe("chat");
    get.mockRestore();
    set.mockRestore();
  });

  it("follows the view: navigating to a Code view leaves Chat, and to chat enters it", async () => {
    const useUI = await fresh();
    useUI.getState().setView("chat");
    expect(useUI.getState().mode).toBe("chat");
    useUI.getState().setView("agents");
    expect(useUI.getState()).toMatchObject({ mode: "code", view: "agents", lastCodeView: "agents" });
  });

  it("keeps settings reachable from both modes without losing the mode or the remembered view", async () => {
    const useUI = await fresh();
    useUI.getState().setView("review");
    useUI.getState().setMode("chat");
    useUI.getState().openSettings("ai");
    expect(useUI.getState()).toMatchObject({ mode: "chat", view: "settings" });
    useUI.getState().setView("settings");
    expect(useUI.getState()).toMatchObject({ mode: "chat", view: "settings", lastCodeView: "review" });
    useUI.getState().setMode("code");
    expect(useUI.getState()).toMatchObject({ mode: "code", view: "review" });
  });

  it("puts Automations in Bots mode and remembers it as the Bots view", async () => {
    const useUI = await fresh();
    useUI.getState().setView("review");
    useUI.getState().setView("automations");
    expect(useUI.getState()).toMatchObject({ mode: "chat", view: "automations", lastCodeView: "review" });
    expect(localStorage.getItem("orole.mode")).toBe("chat");
    useUI.getState().setMode("code");
    expect(useUI.getState().view).toBe("review");
    useUI.getState().setMode("chat");
    expect(useUI.getState().view).toBe("automations");
  });

  it("focusAutomation opens the Automations view with the flow handed off", async () => {
    const useUI = await fresh();
    useUI.getState().focusAutomation("nightly");
    expect(useUI.getState()).toMatchObject({ mode: "chat", view: "automations", automationFocus: "nightly" });
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
