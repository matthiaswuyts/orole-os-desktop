import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import type { ContextBreakdown, ContextUsage } from "@/mygo";
import { ComposerContext, contextCells, contextUsage } from "./ComposerContext";

afterEach(cleanup);
const usage: ContextUsage = { source: "provider", at: "2026-10-09T00:00:00Z", inputTokens: 4000, outputTokens: 1500, cachedTokens: 500 };
const breakdown: ContextBreakdown = {
  model: "claude-opus-5-5", usedTokens: 115_600, windowTokens: 200_000, autoCompactTokens: 167_000, at: "2026-10-09T00:00:00Z",
  categories: [
    { name: "System prompt", tokens: 3_100, kind: "used" },
    { name: "Memory files", tokens: 300, kind: "used", items: [{ name: "C:\\repo\\CLAUDE.md", detail: "Project", tokens: 300 }] },
    { name: "Messages", tokens: 112_200, kind: "used", items: [{ name: "Tool results", tokens: 100_000 }, { name: "User messages", tokens: 12_200 }] },
    { name: "MCP tools (deferred)", tokens: 50_000, kind: "deferred" },
    { name: "Autocompact buffer", tokens: 33_000, kind: "buffer" },
    { name: "Free space", tokens: 51_400, kind: "free" },
  ],
};

it("never turns billed counters into context occupancy or a guessed model limit", () => {
  expect(contextUsage(usage)).toEqual({ used: null, limit: null, fraction: null });
  expect(contextUsage({ ...usage, contextWindow: 10000 })).toEqual({ used: null, limit: 10000, fraction: null });
  render(<ComposerContext usage={usage} />);
  fireEvent.click(screen.getByRole("button", { name: "Context usage: not fully reported" }));
  expect(screen.getByText("Not reported")).toBeInTheDocument();
  expect(screen.getByText("This harness has not reported both the occupancy and the window size.")).toBeInTheDocument();
  expect(screen.queryByRole("list", { name: "Context breakdown" })).not.toBeInTheDocument();
});

it("distinguishes measured zero, missing usage, invalid denominators and over-limit occupancy", () => {
  expect(contextUsage(null)).toEqual({ used: null, limit: null, fraction: null });
  expect(contextUsage({ ...usage, contextTokens: 0, contextWindow: 10000 }).fraction).toBe(0);
  expect(contextUsage({ ...usage, contextTokens: 0, contextWindow: 0 }).fraction).toBeNull();
  expect(contextUsage({ ...usage, contextTokens: -1, contextWindow: 10000 }).fraction).toBeNull();
  expect(contextUsage({ ...usage, contextTokens: 12000, contextWindow: 10000 }).fraction).toBe(1.2);
  render(<ComposerContext usage={{ ...usage, contextTokens: 12000, contextWindow: 10000 }} />);
  fireEvent.click(screen.getByRole("button", { name: "Context usage: 120%" }));
  expect(screen.getByRole("img", { name: "120% of the context window used" })).toBeInTheDocument();
  expect(screen.getByText("This harness reports totals only, so there is no per-category breakdown.")).toBeInTheDocument();
});

it("shows the harness breakdown: used rows with contributors, the buffer, and on-demand rows kept out of the total", () => {
  render(<ComposerContext usage={{ ...usage, contextTokens: 115_600, contextWindow: 200_000, breakdown }} />);
  fireEvent.click(screen.getByRole("button", { name: "Context usage: 58%" }));
  expect(screen.getByText("claude-opus-5-5")).toBeInTheDocument();
  expect(screen.getByText("84.4K left")).toBeInTheDocument();
  expect(screen.getByText("Auto-compacts at 167K")).toBeInTheDocument();
  const list = screen.getByRole("list", { name: "Context breakdown" });
  expect(within(list).getByText("Autocompact buffer")).toBeInTheDocument();
  expect(within(list).getByText("Loaded on demand, not in context")).toBeInTheDocument();
  expect(within(list).queryByText("Free space")).not.toBeInTheDocument();
  fireEvent.click(within(list).getByRole("button", { name: /Memory files/ }));
  expect(within(list).getByText("CLAUDE.md")).toBeInTheDocument();
  expect(screen.queryByText(/Breakdown as of the last finished turn/)).not.toBeInTheDocument();
});

it("says when live occupancy has moved past the last breakdown", () => {
  render(<ComposerContext usage={{ ...usage, contextTokens: 120_000, contextWindow: 200_000, breakdown }} />);
  fireEvent.click(screen.getByRole("button", { name: "Context usage: 60%" }));
  expect(screen.getByText("Breakdown as of the last finished turn (115.6K tokens).")).toBeInTheDocument();
});

it("fills the grid in proportion, keeps tiny contributors visible and never overflows", () => {
  const cells = contextCells(breakdown);
  const count = (category: string) => cells.filter((cell) => cell.category === category).length;
  expect(cells).toHaveLength(100);
  expect(count("Memory files")).toBe(1);
  expect(cells.filter((cell) => cell.kind === "used")).toHaveLength(58);
  expect(cells.filter((cell) => cell.kind === "buffer")).toHaveLength(17);
  expect(count("MCP tools (deferred)")).toBe(0);
  const over = contextCells({ ...breakdown, usedTokens: 260_000, categories: [{ name: "Messages", tokens: 250_000, kind: "used" }, { name: "System prompt", tokens: 10, kind: "used" }] });
  expect(over).toHaveLength(100);
  expect(over.filter((cell) => cell.category === "System prompt")).toHaveLength(1);
  expect(over.filter((cell) => cell.kind === "used")).toHaveLength(100);
});
