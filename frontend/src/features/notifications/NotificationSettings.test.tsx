import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Prefs } from "@/mygo";

const mocks = vi.hoisted(() => ({ link: "online", refresh: vi.fn(), startSync: vi.fn(), setKindPref: vi.fn(), loadPrefs: vi.fn(), playSound: vi.fn() }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/lib/audio", () => ({ playSound: mocks.playSound }));
vi.mock("@/stores/daemon", () => ({ useDaemon: Object.assign((selector: (state: { link: string }) => unknown) => selector({ link: mocks.link }), { getState: () => ({ refresh: mocks.refresh }) }) }));
vi.mock("@/stores/notifications", () => ({ useNotifications: vi.fn() }));
import { useNotifications } from "@/stores/notifications";
import { NotificationSettings } from "./NotificationSettings";

function notificationState() {
  const prefs: Prefs = { kinds: {
    approval: { inApp: true, sound: true }, failed: { inApp: true, sound: true }, finished: { inApp: true, sound: false },
    reply: { inApp: false, sound: true }, message: { inApp: true, sound: true },
  } };
  return {
    prefs, prefsLoading: false, prefsSaving: false, prefsError: null as string | null,
    startSync: mocks.startSync, setKindPref: mocks.setKindPref, loadPrefs: mocks.loadPrefs,
  };
}
let state = notificationState();
beforeEach(() => {
  vi.clearAllMocks();
  mocks.link = "online";
  state = notificationState();
  vi.mocked(useNotifications).mockImplementation(() => state);
});
afterEach(cleanup);

const button = (name: string) => screen.getByRole("button", { name });

describe("NotificationSettings", () => {
  it("shows each kind's stored channels", () => {
    render(<NotificationSettings />);
    expect(button("Play a sound for Finished")).toHaveAttribute("aria-pressed", "false");
    expect(button("Show Bot replies in app")).toHaveAttribute("aria-pressed", "false");
    // Hidden kinds make no sound, whatever was stored.
    expect(button("Play a sound for Bot replies")).toHaveAttribute("aria-pressed", "false");
    expect(button("Play a sound for Bot replies")).toBeDisabled();
    expect(button("Show Failed in app")).toHaveAttribute("aria-pressed", "true");
  });

  it("saves one kind at a time and previews a sound being turned on", () => {
    render(<NotificationSettings />);
    fireEvent.click(button("Play a sound for Finished"));
    expect(mocks.setKindPref).toHaveBeenLastCalledWith("finished", { inApp: true, sound: true });
    expect(mocks.playSound).toHaveBeenLastCalledWith("finished", "notification");
    fireEvent.click(button("Show Failed in app"));
    expect(mocks.setKindPref).toHaveBeenLastCalledWith("failed", { inApp: false, sound: true });
    fireEvent.click(button("Play the Failed sound"));
    expect(mocks.playSound).toHaveBeenLastCalledWith("failed", "notification");
  });

  it("never lets approvals be hidden", () => {
    render(<NotificationSettings />);
    expect(button("Show Needs you in app")).toBeDisabled();
    expect(button("Play a sound for Needs you")).toBeEnabled();
  });

  it.each(["offline", "loading", "saving"])("disables changes while %s", (mode) => {
    mocks.link = mode === "offline" ? "offline" : "online";
    state.prefsLoading = mode === "loading";
    state.prefsSaving = mode === "saving";
    render(<NotificationSettings />);
    for (const kind of ["Failed", "Finished", "Flow messages"]) expect(button(`Show ${kind} in app`)).toBeDisabled();
  });

  it("keeps errors and connection recovery visible", () => {
    state.prefsError = "Could not save preferences";
    render(<NotificationSettings />);
    expect(screen.getByText("Could not save preferences")).toBeVisible();
    fireEvent.click(button("Retry connection"));
    expect(mocks.refresh).toHaveBeenCalledTimes(1);
    expect(mocks.loadPrefs).toHaveBeenCalledTimes(1);
  });
});
