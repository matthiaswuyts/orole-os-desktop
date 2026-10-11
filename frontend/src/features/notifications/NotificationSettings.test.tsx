import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Prefs } from "@/mygo";

const mocks = vi.hoisted(() => ({ link: "online", refresh: vi.fn(), startSync: vi.fn(), setAllPrefs: vi.fn(), test: vi.fn(), load: vi.fn(), loadPrefs: vi.fn() }));
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
    nativeError: null as string | null, testing: false, testStatus: null, testError: null as string | null,
    startSync: mocks.startSync, setAllPrefs: mocks.setAllPrefs, test: mocks.test, load: mocks.load, loadPrefs: mocks.loadPrefs,
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
  it("maps each choice to the stored switches, for every alert at once", async () => {
    render(<NotificationSettings />);
    const choice = screen.getByRole("combobox", { name: /Notify me/ });
    expect(choice).toHaveTextContent("Desktop popup + sound");
    fireEvent.pointerDown(choice, { button: 0, pointerType: "mouse" });
    fireEvent.mouseDown(choice, { button: 0 });
    const option = await screen.findByRole("option", { name: "In app + sound" });
    const { promise: settled, resolve } = Promise.withResolvers<void>();
    setTimeout(resolve, 400);
    await settled;
    fireEvent.pointerUp(option, { button: 0, pointerType: "mouse" });
    fireEvent.mouseUp(option, { button: 0 });
    fireEvent.click(option);
    expect(mocks.setAllPrefs).toHaveBeenLastCalledWith({ enabled: true, native: false, sound: true });
  });

  it("sends the real notification test", () => {
    render(<NotificationSettings />);
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
      for (const control of screen.getAllByRole("combobox")) expect(control).toBeDisabled();
    }
    if (mode === "loading") expect(screen.getByRole("status", { name: "Loading notification preferences" })).toBeInTheDocument();
    if (mode === "saving") expect(screen.getByText("Saving preferences…")).toBeInTheDocument();
  });

  it("keeps errors and connection recovery visible", () => {
    state.prefsError = "Could not save preferences";
    state.error = "Source request failed";
    state.nativeError = "Permission denied";
    state.testError = "Test request failed";
    render(<NotificationSettings />);
    for (const text of ["Could not save preferences", "Source request failed", "Test request failed"]) expect(screen.getByText(text)).toBeVisible();
    expect(screen.getByText(/Permission denied/)).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Retry connection" }));
    expect(mocks.refresh).toHaveBeenCalledTimes(1);
    expect(mocks.loadPrefs).toHaveBeenCalledTimes(1);
    expect(mocks.load).toHaveBeenCalledTimes(1);
  });
});
