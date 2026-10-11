import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({ all: vi.fn(), set: vi.fn() }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({ Settings: { all: api.all, set: api.set } }));
import { AgentSafetySettings } from "./AgentSafetySettings";

beforeEach(() => { vi.clearAllMocks(); api.set.mockResolvedValue(undefined); });
afterEach(cleanup);

it("allows YOLO until the daemon setting turns it off, and writes the setting back", async () => {
  api.all.mockResolvedValue({});
  render(<AgentSafetySettings />);
  const toggle = await screen.findByRole("switch", { name: "Allow YOLO mode" });
  await waitFor(() => expect(toggle).toBeEnabled());
  expect(toggle).toBeChecked();
  fireEvent.click(toggle);
  await waitFor(() => expect(api.set).toHaveBeenCalledWith("agents", { yoloDisabled: true }));
  await waitFor(() => expect(toggle).not.toBeChecked());
});

it("shows a stored off setting and reports a failed save without flipping the switch", async () => {
  api.all.mockResolvedValue({ agents: { yoloDisabled: true } });
  api.set.mockRejectedValue(new Error("daemon offline"));
  render(<AgentSafetySettings />);
  const toggle = await screen.findByRole("switch", { name: "Allow YOLO mode" });
  await waitFor(() => expect(toggle).toBeEnabled());
  expect(toggle).not.toBeChecked();
  fireEvent.click(toggle);
  expect(await screen.findByRole("alert")).toHaveTextContent("daemon offline");
  expect(toggle).not.toBeChecked();
});
