import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import type { ContextUsage } from "@/mygo";
import { ComposerContext, contextUsage } from "./ComposerContext";

afterEach(cleanup);
const usage: ContextUsage = { source: "provider", at: "2026-10-09T00:00:00Z", inputTokens: 4000, outputTokens: 1500, cachedTokens: 500 };

it("never turns billed counters into context occupancy or a guessed model limit", () => {
  expect(contextUsage(usage)).toEqual({ used: null, limit: null, fraction: null });
  expect(contextUsage({ ...usage, contextWindow: 10000 })).toEqual({ used: null, limit: 10000, fraction: null });
  render(<ComposerContext usage={usage} />);
  fireEvent.click(screen.getByRole("button", { name: "Context usage: not fully reported" }));
  expect(screen.getAllByText("Not reported")).toHaveLength(2);
  expect(screen.queryByText("System")).not.toBeInTheDocument();
  expect(screen.queryByText("Tools")).not.toBeInTheDocument();
});

it("distinguishes measured zero, missing usage, invalid denominators and over-limit occupancy", () => {
  expect(contextUsage(null)).toEqual({ used: null, limit: null, fraction: null });
  expect(contextUsage({ ...usage, contextTokens: 0, contextWindow: 10000 }).fraction).toBe(0);
  expect(contextUsage({ ...usage, contextTokens: 0, contextWindow: 0 }).fraction).toBeNull();
  expect(contextUsage({ ...usage, contextTokens: -1, contextWindow: 10000 }).fraction).toBeNull();
  expect(contextUsage({ ...usage, contextTokens: 12000, contextWindow: 10000 }).fraction).toBe(1.2);
  render(<ComposerContext usage={{ ...usage, contextTokens: 12000, contextWindow: 10000 }} />);
  expect(screen.getByRole("button", { name: "Context usage: 120%" })).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Context usage: 120%" }));
  expect(screen.getByRole("progressbar", { name: "Context window used" })).toHaveAttribute("aria-valuenow", "100");
});
