import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Prefs } from "@/mygo";

const mocks = vi.hoisted(() => ({ link: "online", refresh: vi.fn(), startSync: vi.fn(), setSourcePref: vi.fn(), test: vi.fn(), load: vi.fn(), loadPrefs: vi.fn() }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/stores/daemon", () => ({ useDaemon: Object.assign((selector: (state: { link: string }) => unknown) => selector({ link: mocks.link }), { getState: () => ({ refresh: mocks.refresh }) }) }));
vi.mock("@/stores/notifications", () => ({ useNotifications: vi.fn() }));
import { useNotifications } from "@/stores/notifications";
import { NotificationSettings } from "./NotificationSettings";

function notificationState() {
  const prefs: Prefs = { default: { enabled: true, native: true, sound: true }, sources: { "custom.source": { enabled: false, native: true, sound: false } } };
  return {
    items: [], sources: [], unread: 0, source: "", unreadOnly: false, loaded: true, loading: false, busy: false,
    error: null as string | null, actionError: null, prefs, prefsLoading: false, prefsSaving: false, prefsError: null as string | null,
    nativeError: null as string | null, nativeStatus: null as string | null, testing: false, testStatus: null, testError: null as string | null,
    startSync: mocks.startSync, setSourcePref: mocks.setSourcePref, test: mocks.test, load: mocks.load, loadPrefs: mocks.loadPrefs,
    filter: vi.fn(), markRead: vi.fn(), dismiss: vi.fn(),
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

describe("NotificationSettings", () => {
  it("keeps source controls independent and resets only custom preferences", () => {
    render(<NotificationSettings />);
    expect(screen.getByRole("switch", { name: "custom.source: Windows notifications" })).toHaveAttribute("aria-disabled", "true");
    expect(screen.getByRole("switch", { name: "custom.source: notification sound" })).toHaveAttribute("aria-disabled", "true");
    expect(screen.queryByRole("button", { name: "system.test: use defaults" })).toBeNull();
    fireEvent.click(screen.getByRole("switch", { name: "system.test: Windows notifications" }));
    expect(mocks.setSourcePref).toHaveBeenLastCalledWith("system.test", { enabled: true, native: false, sound: true });
    fireEvent.click(screen.getByRole("switch", { name: "Default behavior: notification sound" }));
    expect(mocks.setSourcePref).toHaveBeenLastCalledWith(null, { enabled: true, native: true, sound: false });
    fireEvent.click(screen.getByRole("button", { name: "custom.source: use defaults" }));
    expect(mocks.setSourcePref).toHaveBeenLastCalledWith("custom.source", null);
  });

  it("keeps the real notification test and collapses delivery explanations", () => {
    render(<NotificationSettings />);
    expect(screen.getByText("How alerts work").closest("details")).not.toHaveAttribute("open");
    fireEvent.click(screen.getByRole("button", { name: "Test notification" }));
    expect(mocks.test).toHaveBeenCalledTimes(1);
  });

  it.each(["offline", "loading", "saving", "testing"])("disables unsafe actions while %s", (mode) => {
    mocks.link = mode === "offline" ? "offline" : "online";
    state.prefsLoading = mode === "loading";
    state.prefsSaving = mode === "saving";
    state.testing = mode === "testing";
    render(<NotificationSettings />);
    expect(screen.getByRole("button", { name: mode === "testing" ? "Sending test…" : "Test notification" })).toBeDisabled();
    if (mode !== "testing") {
      for (const control of screen.getAllByRole("switch")) {
        expect(control).toHaveAttribute("aria-disabled", "true");
        fireEvent.click(control);
      }
      expect(mocks.setSourcePref).not.toHaveBeenCalled();
      expect(screen.getByRole("button", { name: "custom.source: use defaults" })).toBeDisabled();
    }
    if (mode === "loading") expect(screen.getByRole("status", { name: "Loading notification preferences" })).toBeInTheDocument();
    if (mode === "saving") expect(screen.getByText("Saving preferences…")).toBeInTheDocument();
  });

  it("keeps errors, delivery status, and connection recovery visible", () => {
    state.prefsError = "Could not save preferences";
    state.error = "Source request failed";
    state.nativeError = "Permission denied";
    state.nativeStatus = "Windows delivery requested";
    state.testError = "Test request failed";
    render(<NotificationSettings />);
    for (const text of ["Could not save preferences", "Source request failed", "Windows delivery requested", "Test request failed"]) expect(screen.getByText(text)).toBeVisible();
    expect(screen.getByText(/Permission denied/)).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Retry connection" }));
    expect(mocks.refresh).toHaveBeenCalledTimes(1);
    expect(mocks.loadPrefs).toHaveBeenCalledTimes(1);
    expect(mocks.load).toHaveBeenCalledTimes(1);
  });
});
