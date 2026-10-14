import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ImageGeneration } from "./image-generation";
import { PermissionGrant } from "./permission-grant";

afterEach(cleanup);

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
