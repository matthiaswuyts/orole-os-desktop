import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const bridge = vi.hoisted(() => ({ all: vi.fn(), set: vi.fn() }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({
  Settings: bridge,
  events: { settingsChanged: { on: () => () => {} } },
}));
vi.mock("@/stores/daemon", () => ({ useDaemon: (select: (s: unknown) => unknown) => select({ link: "connected", error: null }) }));
import { FirstRunGuide } from "./FirstRunGuide";
import { useUI } from "@/stores/ui";

afterEach(cleanup);
beforeEach(() => {
  bridge.all.mockReset(); bridge.set.mockReset();
  bridge.set.mockResolvedValue(undefined);
  useUI.setState({ view: "home" });
});

it("keeps the current step and navigation when saving progress fails", async () => {
  bridge.all.mockResolvedValue({ onboarding: { step: 3, dismissed: false } });
  bridge.set.mockRejectedValue(new Error("daemon disconnected"));
  render(<FirstRunGuide />);
  fireEvent.click(await screen.findByRole("button", { name: "Open GitHub setup" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("daemon disconnected");
  expect(screen.getByRole("heading")).toHaveTextContent("Connect GitHub");
  expect(useUI.getState().view).toBe("home");
});

it("honors a saved dismissal and permits explicitly restarting the guide", async () => {
  bridge.all.mockResolvedValue({ onboarding: { step: 6, dismissed: true } });
  render(<FirstRunGuide />);
  const reopen = await screen.findByRole("button", { name: "Reopen setup guide" });
  expect(screen.queryByRole("heading")).toBeNull();
  fireEvent.click(reopen);
  await waitFor(() => expect(screen.getByRole("heading")).toHaveTextContent("Welcome to Orole OS"));
});

it("recovers invalid saved progress without hiding setup", async () => {
  bridge.all.mockResolvedValue({ onboarding: { step: 999, dismissed: false } });
  render(<FirstRunGuide />);
  expect(await screen.findByRole("heading")).toHaveTextContent("Welcome to Orole OS");
});
