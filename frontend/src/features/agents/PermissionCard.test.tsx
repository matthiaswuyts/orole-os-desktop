import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { PermissionRequest } from "@/mygo";

vi.mock("@/mygo", () => ({ Agents: { approve: vi.fn(), get: vi.fn() } }));
vi.mock("@/stores/agents", () => ({ useAgents: { getState: () => ({ upsert: vi.fn() }) } }));
vi.mock("@/stores/boxes", () => ({ useBoxes: vi.fn() }));
import { PermissionCard } from "./shared";

afterEach(cleanup);
const request = (inputJSON: string, id = "snapshot-1"): PermissionRequest => ({ id, tool: "Write", summary: "safe-looking.txt", input: JSON.parse(inputJSON), inputJSON, at: "2026-10-08T00:00:00Z" });

describe("agent approval disclosure", () => {
  it("reveals malicious suffixes, omitted fields, exact numeric input and directional characters before consent", () => {
    const inputJSON = '{"command":' + JSON.stringify("echo " + "padding ".repeat(80) + "; delete-sensitive-target") + ',"file_path":"safe-looking.txt","content":"<img src=x>\\nreplacement\u202e","number":9007199254740993}';
    const { container } = render(<PermissionCard sessionId="session" request={request(inputJSON)} />);
    const detail = screen.getByLabelText("Complete tool arguments");
    expect(detail.textContent).toContain("delete-sensitive-target");
    expect(detail.textContent).toContain('"content":"<img src=x>\\nreplacement\\u202e"');
    expect(detail.textContent).toContain("9007199254740993");
    expect(detail.textContent).not.toContain("\u202e");
    expect(container.querySelector("img")).toBeNull();
    expect(screen.getByRole("button", { name: "Allow" })).toBeDisabled();
    fireEvent.click(screen.getByRole("checkbox", { name: "I reviewed the complete tool name and arguments." }));
    expect(screen.getByRole("button", { name: "Allow" })).toBeEnabled();
  });

  it("requires new review for a replacement snapshot and leaves denial available without disclosure", () => {
    const view = render(<PermissionCard sessionId="session" request={request('{"command":"echo first"}')} />);
    fireEvent.click(screen.getByRole("checkbox"));
    view.rerender(<PermissionCard sessionId="session" request={request('{"command":"echo second"}', "snapshot-2")} />);
    expect(screen.getByRole("checkbox")).not.toBeChecked();
    expect(screen.getByRole("button", { name: "Allow" })).toBeDisabled();
    expect(screen.getByLabelText("Complete tool arguments").textContent).toContain("echo second");
    view.rerender(<PermissionCard sessionId="session" request={{ ...request("{}"), inputJSON: "" }} />);
    expect(screen.getByRole("button", { name: "Allow" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Deny" })).toBeEnabled();
    expect(screen.getByRole("alert")).toHaveTextContent("complete input is unavailable");
  });
});
