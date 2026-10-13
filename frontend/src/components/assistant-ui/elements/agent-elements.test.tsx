import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SubagentList } from "./subagent-list";
import { TodoList, todoProgress, type TodoItem } from "./todo-list";

afterEach(cleanup);

const items: TodoItem[] = [
  { id: "1", text: "Read the failing test", status: "done", description: "Trace the current behaviour." },
  { id: "2", text: "Reproduce locally", status: "active" },
  { id: "3", text: "Write the fix", status: "pending" },
  { id: "4", text: "Re-run the suite", status: "failed", reason: "Tests timed out", description: "Full suite" },
  { id: "5", text: "Update the old fixture", status: "cancelled" },
];

describe("TodoList", () => {
  it("counts done over everything but cancelled: failed stays in the denominator, cancelled leaves both", () => {
    expect(todoProgress(items)).toEqual({ done: 1, total: 4 });
    expect(todoProgress([{ status: "done" }, { status: "done" }, { status: "failed" }])).toEqual({ done: 2, total: 3 });
    expect(todoProgress([{ status: "done" }, { status: "done" }, { status: "cancelled" }])).toEqual({ done: 2, total: 2 });
    expect(todoProgress([])).toEqual({ done: 0, total: 0 });
  });

  it("announces each status, shows a failed item's reason over its description and strikes settled rows", () => {
    render(<TodoList items={items} revision={3} />);
    expect(screen.getByText("1/4 · rev 3")).toBeInTheDocument();
    const rows = within(screen.getByRole("list", { name: "Todos" })).getAllByRole("listitem");
    expect(rows.map((row) => row.getAttribute("data-status"))).toEqual(["done", "active", "pending", "failed", "cancelled"]);
    expect(rows[3]).toHaveTextContent("Tests timed out");
    expect(rows[3]).not.toHaveTextContent("Full suite");
    expect(rows[0]).toHaveTextContent("Done:");
    expect(within(rows[0]!).getByText("Read the failing test")).toHaveClass("line-through");
    expect(within(rows[2]!).getByText("Write the fix")).not.toHaveClass("line-through");
  });

  it("folds past maxVisible but keeps later active and failed rows, and unfolds on demand", () => {
    render(<TodoList maxVisible={1} items={items} />);
    const list = screen.getByRole("list", { name: "Todos" });
    expect(within(list).getAllByRole("listitem").map((row) => row.getAttribute("data-status"))).toEqual(["done", "active", "failed"]);
    fireEvent.click(screen.getByRole("button", { name: "Show 2 more" }));
    expect(within(list).getAllByRole("listitem")).toHaveLength(5);
    expect(screen.queryByRole("button")).toBeNull();
  });
});

describe("SubagentList", () => {
  it("renders one pressable row per worker, marks the selected one and reports clicks by index", () => {
    const onOpen = vi.fn();
    render(<SubagentList aria-label="Workers" selectedIndex={1} onOpen={onOpen} agents={[
      { name: "Explore the runtime", model: "haiku", status: "running", activity: "Reading files" },
      { name: "Fix composer types", model: "sonnet", status: "done" },
      { name: "Write tests", model: "sonnet", status: "failed", progress: 40 },
    ]} summary={{ name: "Summarize findings", model: "haiku", status: "running" }} />);
    const rows = within(screen.getByRole("list", { name: "Workers" })).getAllByRole("button");
    expect(rows).toHaveLength(3);
    expect(rows.map((row) => row.getAttribute("aria-pressed"))).toEqual(["false", "true", "false"]);
    expect(rows[0]).toHaveTextContent("Reading files");
    expect(within(rows[0]!).getByRole("progressbar")).not.toHaveAttribute("aria-valuenow");
    expect(within(rows[1]!).getByRole("progressbar", { name: "Fix composer types progress" })).toHaveAttribute("aria-valuenow", "100");
    expect(within(rows[2]!).getByRole("progressbar")).toHaveAttribute("aria-valuenow", "40");
    fireEvent.click(rows[2]!);
    expect(onOpen).toHaveBeenCalledWith(2);
    expect(screen.getByText("Summarize findings").closest("button")).toBeNull();
  });
});
