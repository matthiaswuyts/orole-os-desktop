import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { Terminal } from "lucide-react";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ImageGeneration } from "./image-generation";
import { PermissionGrant } from "./permission-grant";
import { RunActivity, type RunEntry, type RunStatus } from "./run-activity";

afterEach(cleanup);

const entries: RunEntry[] = [
  { id: "c1", kind: "commentary", label: "Inspecting the files", content: <p>I will inspect the files.</p> },
  { id: "t1", kind: "tool", label: "Ran", chip: "npm test", icon: Terminal, title: "10/8/2026, 12:00:00 PM" },
  { id: "t2", kind: "tool", label: "Ran", chip: "npm run lint", icon: Terminal, failed: true },
];

function Run({ status = "complete", durationLabel = "2m 13s", statusLabel = "Worked for", list = entries }: { status?: RunStatus; durationLabel?: string; statusLabel?: string; list?: RunEntry[] }) {
  const [open, setOpen] = useState(false);
  return <RunActivity status={status} statusLabel={statusLabel} durationLabel={durationLabel} open={open} onOpenChange={setOpen} entries={list} attention={<p>Needs approval</p>}><p>The final answer.</p></RunActivity>;
}

describe("RunActivity", () => {
  it("collapses commentary and tools behind the status row while the answer and attention slot stay visible", () => {
    render(<Run />);
    const trigger = screen.getByRole("button", { name: /Worked for.*2m 13s/ });
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    expect(screen.getByText("The final answer.")).toBeInTheDocument();
    expect(screen.getByText("Needs approval")).toBeInTheDocument();
    expect(screen.queryByText("npm test")).toBeNull();
    fireEvent.click(trigger);
    const rows = within(screen.getByRole("list", { name: "Activity" })).getAllByRole("listitem");
    expect(rows.map((row) => row.textContent)).toEqual(["Inspecting the files", "Rannpm test", "Rannpm run lintFailed"]);
    expect(rows[1]).toHaveAttribute("title", "10/8/2026, 12:00:00 PM");
    expect(rows[2]).toHaveAttribute("data-failed", "true");
    expect(trigger).toHaveAttribute("aria-expanded", "true");
  });

  it("shows only a status for an empty run, announces the duration once settled and never while running", () => {
    const view = render(<Run list={[]} status="running" statusLabel="Working" durationLabel="" />);
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.getByRole("status")).toHaveTextContent("Working");
    expect(view.container.querySelector("[data-slot=run-activity]")).toHaveAttribute("aria-busy", "true");
    view.rerender(<Run list={[]} status="complete" />);
    expect(screen.getByRole("status")).toHaveTextContent("Worked for 2m 13s");
  });

  it("does not present an interrupted run as a success", () => {
    render(<Run status="error" statusLabel="Failed after" />);
    expect(screen.getByRole("button", { name: /Failed after/ }).closest("[data-slot=run-activity]")).toHaveAttribute("data-status", "error");
  });
});

describe("PermissionGrant", () => {
  const reach = [{ label: "Command", value: "git status" }, "C:\\work"];

  it("renders the capability, requester and reach, and reports each choice once", () => {
    const onGrant = vi.fn();
    render(<PermissionGrant capability="Run a command" requester="Codex" reach={reach} scope="pending" onGrant={onGrant} />);
    expect(screen.getByText("requested by Codex")).toBeInTheDocument();
    expect(screen.getByLabelText("Command")).toHaveTextContent("git status");
    expect(screen.getByText("C:\\work")).toBeInTheDocument();
    expect(screen.getAllByRole("button").map((button) => button.textContent)).toEqual(["Deny", "Allow once"]);
    fireEvent.click(screen.getByRole("button", { name: "Allow once" }));
    expect(onGrant).toHaveBeenCalledExactlyOnceWith("once");
  });

  it("offers only the scopes it is given, and no buttons without a handler or once resolved", () => {
    const view = render(<PermissionGrant capability="Run a command" requester="Codex" reach={reach} scope="pending" onGrant={() => {}} grants={["once", "session"]} />);
    expect(screen.getByRole("button", { name: "This session" })).toBeInTheDocument();
    view.rerender(<PermissionGrant capability="Run a command" requester="Codex" reach={reach} scope="pending" />);
    expect(screen.queryByRole("button")).toBeNull();
    view.rerender(<PermissionGrant capability="Run a command" requester="Codex" reach={reach} scope="denied" onGrant={() => {}} />);
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.getByRole("status")).toHaveTextContent("Denied");
  });

  it("disables allowing a blocked request but keeps Deny, and renders reach as inert text", () => {
    const { container } = render(<PermissionGrant capability="Edit files" requester="Codex" reach={["<img src=x onerror=alert(1)>"]} scope="pending" onGrant={() => {}} blocked="Cannot be approved." />);
    expect(screen.getByRole("alert")).toHaveTextContent("Cannot be approved.");
    expect(screen.getByRole("button", { name: "Allow once" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Deny" })).toBeEnabled();
    expect(container.querySelector("img")).toBeNull();
  });
});

describe("ImageGeneration", () => {
  it("shows a shimmering Generating label with the prompt and never an image or resolution", () => {
    const { container } = render(<ImageGeneration prompt="A calm lake" generating />);
    expect(screen.getByRole("status")).toHaveTextContent("Generating");
    expect(screen.getByText("A calm lake")).toBeInTheDocument();
    expect(container.querySelector("img")).toBeNull();
    expect(container).not.toHaveTextContent(/1024/);
    expect(container.querySelectorAll("[aria-hidden] span.animate-pulse")).toHaveLength(64);
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("offers regeneration only when the host supplies an action, and not while generating", () => {
    const regenerate = vi.fn();
    const view = render(<ImageGeneration prompt="A calm lake" generating={false} onRegenerate={regenerate} />);
    fireEvent.click(screen.getByRole("button", { name: "Regenerate image" }));
    expect(regenerate).toHaveBeenCalledTimes(1);
    view.rerender(<ImageGeneration prompt="A calm lake" generating onRegenerate={regenerate} />);
    expect(screen.queryByRole("button", { name: "Regenerate image" })).toBeNull();
  });
});
