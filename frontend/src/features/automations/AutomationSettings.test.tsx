import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Config } from "./api";

const mocks = vi.hoisted(() => ({ link: "online", config: vi.fn(), setConfig: vi.fn(), generateSecret: vi.fn() }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/stores/daemon", () => ({ useDaemon: (selector: (state: { link: string }) => unknown) => selector({ link: mocks.link }) }));
vi.mock("./api", () => ({ api: mocks, errorText: (error: unknown) => error instanceof Error ? error.message : String(error) }));
import { AutomationSettings } from "./AutomationSettings";

const config: Config = { globalConcurrency: 8, perAutomation: 2, webhookEnabled: false, webhookPort: 8765, hasSecret: true, retentionDays: 0, retentionMaxRuns: 0 };
beforeEach(() => {
  vi.clearAllMocks();
  mocks.link = "online";
  mocks.config.mockResolvedValue({ ...config });
  mocks.setConfig.mockImplementation((next: Config) => Promise.resolve(next));
  mocks.generateSecret.mockResolvedValue(undefined);
});
afterEach(cleanup);

async function renderSettings() {
  const view = render(<AutomationSettings />);
  await screen.findByRole("spinbutton", { name: "Across all automations" });
  return view;
}

describe("AutomationSettings", () => {
  it("groups editable limits and hides connection instructions until expanded", async () => {
    await renderSettings();
    const details = screen.getByText("Webhook connection details").closest("details");
    expect(details).not.toHaveAttribute("open");
    expect(screen.getByText("automations/webhook-hmac")).not.toBeVisible();
    fireEvent.click(screen.getByText("Webhook connection details"));
    expect(screen.getByLabelText("Webhook port")).toBeVisible();
    expect(screen.getByText("automations/webhook-hmac")).toBeVisible();
    fireEvent.change(screen.getByRole("spinbutton", { name: "Across all automations" }), { target: { value: "6" } });
    fireEvent.change(screen.getByRole("spinbutton", { name: "Per automation (default)" }), { target: { value: "3" } });
    fireEvent.change(screen.getByRole("spinbutton", { name: "Webhook port" }), { target: { value: "9000" } });
    fireEvent.click(screen.getByRole("switch", { name: "Local webhooks" }));
    expect(mocks.setConfig).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Save settings" }));
    await waitFor(() => expect(mocks.setConfig).toHaveBeenCalledWith({ ...config, globalConcurrency: 6, perAutomation: 3, webhookEnabled: true, webhookPort: 9000 }));
    expect(await screen.findByText("Automation settings saved.")).toBeVisible();
  });

  it("does not silently clamp a per-automation limit when the global limit is reduced", async () => {
    await renderSettings();
    fireEvent.change(screen.getByRole("spinbutton", { name: "Across all automations" }), { target: { value: "1" } });
    expect(screen.getByRole("spinbutton", { name: "Per automation (default)" })).toHaveValue(2);
    expect(mocks.setConfig).not.toHaveBeenCalled();
  });

  it("requires confirmation for rotation and preserves unsaved settings through the refresh", async () => {
    await renderSettings();
    fireEvent.change(screen.getByRole("spinbutton", { name: "Across all automations" }), { target: { value: "5" } });
    fireEvent.click(screen.getByRole("button", { name: "Replace secret…" }));
    expect(mocks.generateSecret).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Keep current secret" }));
    expect(screen.queryByText("Replace the webhook secret?")).toBeNull();
    expect(mocks.generateSecret).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Replace secret…" }));
    fireEvent.click(screen.getByRole("button", { name: "Replace secret" }));
    expect(await screen.findByText("Webhook secret saved securely.")).toBeVisible();
    expect(mocks.generateSecret).toHaveBeenCalledTimes(1);
    expect(mocks.setConfig).not.toHaveBeenCalled();
    expect(screen.getByRole("spinbutton", { name: "Across all automations" })).toHaveValue(5);
    expect(screen.queryByText("Replace the webhook secret?")).toBeNull();
  });

  it("generates the first secret without a replacement confirmation", async () => {
    mocks.config.mockResolvedValueOnce({ ...config, hasSecret: false });
    await renderSettings();
    expect(screen.getByText("Generate a secret before enabling webhooks.")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Generate secret" }));
    expect(await screen.findByRole("button", { name: "Replace secret…" })).toBeVisible();
    expect(mocks.generateSecret).toHaveBeenCalledTimes(1);
    expect(screen.queryByText("Replace the webhook secret?")).toBeNull();
  });

  it("blocks changes offline but allows cancelling secret replacement", async () => {
    const view = await renderSettings();
    fireEvent.click(screen.getByRole("button", { name: "Replace secret…" }));
    mocks.link = "offline";
    view.rerender(<AutomationSettings />);
    expect(screen.getByText("Daemon offline")).toBeVisible();
    expect(screen.getByRole("spinbutton", { name: "Across all automations" })).toBeDisabled();
    expect(screen.getByRole("switch", { name: "Local webhooks" })).toHaveAttribute("aria-disabled", "true");
    expect(screen.getByRole("button", { name: "Save settings" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Replace secret" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Keep current secret" }));
    expect(screen.queryByText("Replace the webhook secret?")).toBeNull();
  });

  it("keeps the draft after a save failure and allows retry", async () => {
    mocks.setConfig.mockRejectedValueOnce(new Error("Could not save limits"));
    await renderSettings();
    fireEvent.change(screen.getByRole("spinbutton", { name: "Across all automations" }), { target: { value: "7" } });
    fireEvent.click(screen.getByRole("button", { name: "Save settings" }));
    expect(await screen.findByText("Could not save limits")).toBeVisible();
    expect(screen.getByRole("spinbutton", { name: "Across all automations" })).toHaveValue(7);
    fireEvent.click(screen.getByRole("button", { name: "Save settings" }));
    expect(await screen.findByText("Automation settings saved.")).toBeVisible();
  });

  it("shows loading and recovers from an initial load failure", async () => {
    mocks.config.mockRejectedValueOnce(new Error("Connection lost"));
    render(<AutomationSettings />);
    expect(screen.getByRole("status")).toHaveTextContent("Loading automation settings…");
    expect(await screen.findByText("Connection lost")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByRole("spinbutton", { name: "Across all automations" })).toHaveValue(8);
  });
});
