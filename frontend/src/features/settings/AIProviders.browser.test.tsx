import { render, renderHook, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";

vi.mock("@/lib/backend", () => ({ inDesktop: false }));
vi.mock("@/mygo", () => ({ LLM: {}, Daemon: {}, Shell: {}, events: {} }));
import { AIProviders } from "@/features/settings/AIProviders";
import { useHasProvider } from "@/stores/llm";

it("shows the desktop-required state in the browser preview, with no provider calls", () => {
  render(<AIProviders />);
  expect(screen.getByText("AI providers needs the desktop app")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Connect" })).toBeNull();
});

it("reports an unknown provider state outside the desktop app", () => {
  expect(renderHook(() => useHasProvider()).result.current).toBeNull();
});
