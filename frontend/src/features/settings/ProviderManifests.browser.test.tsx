import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";

vi.mock("@/lib/backend", () => ({ inDesktop: false }));
vi.mock("@/mygo", () => ({ LLM: {}, Daemon: {}, Shell: {}, events: {} }));
import { AIProviders } from "@/features/settings/AIProviders";
import { useLLM } from "@/stores/llm";

it("offers no provider manifest actions in the browser preview and makes no daemon calls", async () => {
  render(<AIProviders />);
  expect(screen.getByText("AI providers needs the desktop app")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Add provider" })).toBeNull();
  await useLLM.getState().loadManifests();
  expect(useLLM.getState().manifestsReady).toBe(false);
});
