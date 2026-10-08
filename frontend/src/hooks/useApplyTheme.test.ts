import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

vi.mock("@/lib/backend", () => ({ inDesktop: false, previewKey: "theme-test-settings" }));
vi.mock("@/mygo", () => ({ Settings: {}, events: {} }));

import { useApplyTheme } from "./useApplyTheme";
import { defaults, useSettings } from "@/stores/settings";
import { BUILTIN_TOKEN_KEYS, MIDNIGHT } from "@/themes/builtin";

beforeEach(() => {
  localStorage.clear();
  useSettings.setState({ ...structuredClone(defaults), loaded: false, error: null });
});
afterEach(() => {
  cleanup();
  for (const key of BUILTIN_TOKEN_KEYS) document.documentElement.style.removeProperty(key);
  document.documentElement.style.removeProperty("color-scheme");
  document.documentElement.classList.remove("dark");
  delete document.documentElement.dataset.density;
});

it("restores a saved light palette without retaining dark mode", async () => {
  renderHook(useApplyTheme);
  act(() => useSettings.getState().set("appearance", { theme: "solarized-light" }));
  await waitFor(() => {
    const saved = JSON.parse(localStorage.getItem("theme-test-settings") ?? "{}");
    expect(saved.appearance.theme).toBe("solarized-light");
  });
  act(() => useSettings.setState({ appearance: structuredClone(defaults.appearance) }));
  await act(async () => { await useSettings.getState().load(); });
  expect(useSettings.getState().appearance.theme).toBe("solarized-light");
  expect(document.documentElement.classList.contains("dark")).toBe(false);
  expect(document.documentElement.style.colorScheme).toBe("light");
  expect(document.documentElement.style.getPropertyValue("--background")).toBe("#fdf6e3");
});

it("removes expanded palette tokens when returning to defaults or custom themes", () => {
  renderHook(useApplyTheme);
  act(() => useSettings.setState({ appearance: { ...defaults.appearance, theme: "catppuccin-mocha" } }));
  expect(document.documentElement.style.getPropertyValue("--chart-1")).toBe("#89b4fa");
  act(() => useSettings.setState({ appearance: { ...defaults.appearance, theme: "custom", customTokens: { primary: "#123456" } } }));
  expect(document.documentElement.style.getPropertyValue("--primary")).toBe("#123456");
  expect(document.documentElement.style.getPropertyValue("--background")).toBe(MIDNIGHT["--background"]);
  expect(document.documentElement.style.getPropertyValue("--chart-1")).toBe("");
  act(() => useSettings.setState({ appearance: { ...defaults.appearance, theme: "light" } }));
  for (const key of BUILTIN_TOKEN_KEYS) expect(document.documentElement.style.getPropertyValue(key)).toBe("");
  expect(document.documentElement.classList.contains("dark")).toBe(false);
});
