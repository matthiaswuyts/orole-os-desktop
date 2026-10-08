import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { PermissionRequest, Question, QuestionRequest, Session } from "@/mygo";

const api = vi.hoisted(() => ({ approve: vi.fn(), get: vi.fn(), answerInput: vi.fn(), upsert: vi.fn() }));
vi.mock("@/mygo", () => ({ Agents: api }));
vi.mock("@/stores/agents", () => ({ useAgents: { getState: () => ({ upsert: api.upsert }) } }));
import { QuestionCard, SubagentCard, TaskApprovalCard } from "./TaskInteractions";
import { questionAnswers, questionRequestKey, validateQuestionAnswer } from "./questionState";

const question: Question = { id: "scope", header: "Scope", prompt: "Which scope?", multiple: false, allowText: false, secret: false, options: [{ id: "small", label: "Small", description: "" }, { id: "large", label: "Large", description: "" }] };
const request: QuestionRequest = {
  id: "request-1", at: "2026-10-09T00:00:00Z", questions: [question,
    { id: "checks", header: "Checks", prompt: "Which checks?", multiple: true, allowText: true, secret: false, options: [{ id: "unit", label: "Unit tests", description: "" }, { id: "lint", label: "Lint", description: "" }] },
    { id: "token", header: "Authentication", prompt: "Provider token?", multiple: false, allowText: true, secret: true, options: [] },
  ],
};
const session = { id: "session", live: true, mode: "structured", machine: "local", questions: [request], pending: [] } as unknown as Session;
const permission: PermissionRequest = { id: "permission-1", tool: "Write", summary: "safe-looking.txt", input: {}, inputJSON: '{"file_path":"safe-looking.txt"}', at: "2026-10-09T00:00:00Z" };
beforeEach(() => { vi.clearAllMocks(); api.approve.mockResolvedValue(undefined); api.get.mockResolvedValue(session); api.answerInput.mockResolvedValue({ ...session, questions: [] }); });
afterEach(() => { cleanup(); vi.useRealTimers(); });

describe("verified question flow", () => {
  it("submits every validated answer together, preserves rejected drafts and hides secret receipts", async () => {
    api.answerInput.mockRejectedValueOnce(new Error("Provider disconnected"));
    render(<QuestionCard session={session} request={request} />);
    expect(screen.queryByLabelText("Other answer")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("radio", { name: "Small" }));
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Unit tests" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Lint" }));
    fireEvent.change(screen.getByLabelText("Other answer"), { target: { value: "Manual check" } });
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(api.answerInput).not.toHaveBeenCalled();
    const secret = screen.getByLabelText("Your answer (secret)");
    expect(secret).toHaveAttribute("type", "password");
    fireEvent.change(secret, { target: { value: "  literal secret  " } });
    fireEvent.click(screen.getByRole("button", { name: "Submit answers" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Provider disconnected");
    expect(secret).toHaveValue("  literal secret  ");
    expect(api.answerInput).toHaveBeenCalledWith("session", "request-1", [
      { questionId: "scope", values: ["small"] }, { questionId: "checks", values: ["unit", "lint", "Manual check"] }, { questionId: "token", values: ["  literal secret  "] },
    ], false);
    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    expect(screen.getByRole("checkbox", { name: "Unit tests" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Lint" })).toBeChecked();
    expect(screen.getByLabelText("Other answer")).toHaveValue("Manual check");
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    fireEvent.click(screen.getByRole("button", { name: "Submit answers" }));
    await screen.findByText("Secret answer submitted");
    expect(screen.queryByDisplayValue("  literal secret  ")).not.toBeInTheDocument();
    expect(screen.queryByText(/literal secret/)).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Submit answers" })).not.toBeInTheDocument();
  });

  it("does not treat stale event history or recycled request IDs as actionable", () => {
    const view = render(<QuestionCard session={{ ...session, questions: [] }} request={request} />);
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(screen.getByText(/History is read-only/)).toBeInTheDocument();
    view.rerender(<QuestionCard session={{ ...session, questions: [{ ...request, questions: [{ ...question, prompt: "Changed question" }] }] }} request={request} />);
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(api.answerInput).not.toHaveBeenCalled();
  });

  it("expires open requests and supports daemon cancellation without partial answers", async () => {
    const view = render(<QuestionCard session={session} request={request} />);
    fireEvent.click(screen.getByRole("button", { name: "Cancel request" }));
    await waitFor(() => expect(api.answerInput).toHaveBeenCalledWith("session", "request-1", [], true));
    await screen.findByText("Cancellation confirmed by the daemon.");
    view.unmount();
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-10-09T00:00:00Z"));
    const expires = { ...request, expiresAt: "2026-10-09T00:00:01Z" };
    render(<QuestionCard session={{ ...session, questions: [expires] }} request={expires} />);
    act(() => { vi.advanceTimersByTime(1001); });
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(screen.getByText("This question request expired.")).toBeInTheDocument();
  });

  it("rejects incomplete, duplicate and unsupported answers rather than submitting partial data", () => {
    expect(validateQuestionAnswer(question, { choices: [], text: "" })).toMatch(/Choose an option/);
    expect(validateQuestionAnswer(question, { choices: ["unknown"], text: "" })).toMatch(/no longer available/);
    expect(validateQuestionAnswer(question, { choices: ["small", "large"], text: "" })).toBe("Choose one answer.");
    expect(validateQuestionAnswer(question, { choices: ["small", "small"], text: "" })).toMatch(/only once/);
    expect(validateQuestionAnswer(question, { choices: [], text: "injected text" })).toMatch(/only the listed choices/);
    expect(() => questionAnswers(request, { scope: { choices: ["small"], text: "" } })).toThrow();
    expect(() => questionAnswers({ ...request, questions: [question, question] }, { scope: { choices: ["small"], text: "" } })).toThrow(/duplicate IDs/);
    expect(() => questionAnswers({ ...request, questions: [{ ...question, id: "constructor" }] }, {})).toThrow(/Choose an option/);
  });

  it("matches equivalent daemon and transcript snapshots regardless of object property order", () => {
    const reordered = { ...request, questions: request.questions.map(({ id, ...rest }) => ({ ...rest, id })) };
    expect(questionRequestKey(reordered)).toBe(questionRequestKey(request));
    render(<QuestionCard session={{ ...session, questions: [reordered] }} request={request} />);
    expect(screen.getByRole("button", { name: "Next" })).toBeEnabled();
  });
});

describe("tool permission decisions", () => {
  it("discloses complete escaped input and requires reviewing each exact snapshot", () => {
    const inputJSON = '{"command":' + JSON.stringify("padding ".repeat(80) + "; delete-sensitive-target") + ',"content":"<img src=x>\nreplacement\u202e","number":9007199254740993}';
    const view = render(<TaskApprovalCard sessionId="session" request={{ ...permission, tool: "Write\u202e", inputJSON }} />);
    expect(screen.getByLabelText("Complete tool arguments")).toHaveTextContent("delete-sensitive-target");
    expect(screen.getByLabelText("Complete tool arguments").textContent).toContain("9007199254740993");
    expect(screen.getByLabelText("Complete tool arguments").textContent).toContain("\\u202e");
    expect(screen.getByLabelText("Complete tool arguments").textContent).not.toContain("\u202e");
    expect(view.container.querySelector("img")).toBeNull();
    expect(screen.getByRole("button", { name: "Allow once" })).toBeDisabled();
    fireEvent.click(screen.getByRole("checkbox"));
    expect(screen.getByRole("button", { name: "Allow once" })).toBeEnabled();
    view.rerender(<TaskApprovalCard sessionId="session" request={{ ...permission, inputJSON: '{"command":"changed under same ID"}' }} />);
    expect(screen.getByRole("checkbox")).not.toBeChecked();
    expect(screen.getByRole("button", { name: "Allow once" })).toBeDisabled();
    view.rerender(<TaskApprovalCard sessionId="session" request={{ ...permission, inputJSON: "" }} />);
    expect(screen.getByRole("button", { name: "Deny" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Allow once" })).toBeDisabled();
  });

  it("records acknowledged denial even when refreshing session fails", async () => {
    api.get.mockRejectedValue(new Error("Refresh disconnected"));
    render(<TaskApprovalCard sessionId="session" request={permission} />);
    fireEvent.change(screen.getByLabelText("Reason (optional)"), { target: { value: "Not this file" } });
    fireEvent.click(screen.getByRole("button", { name: "Deny" }));
    await screen.findByText("Denied");
    expect(api.approve).toHaveBeenCalledWith("session", "permission-1", false, "Not this file");
    expect(screen.getByRole("status")).toHaveTextContent("Reason: Not this file");
    expect(await screen.findByRole("alert")).toHaveTextContent("Refresh disconnected");
    expect(screen.queryByRole("button", { name: "Deny" })).not.toBeInTheDocument();
  });

  it("does not invent approval receipts from missing pending state or rejected APIs", async () => {
    const view = render(<TaskApprovalCard sessionId="session" request={permission} resolved />);
    expect(screen.getByRole("status")).toHaveTextContent("No decision receipt is available");
    expect(screen.queryByText("Allowed once")).not.toBeInTheDocument();
    view.rerender(<TaskApprovalCard sessionId="session" request={permission} resolved decision={{ allow: true, reason: "Reviewed" }} />);
    expect(screen.getByRole("status")).toHaveTextContent("Allowed once");
    view.unmount();
    api.approve.mockRejectedValue(new Error("Permission expired"));
    render(<TaskApprovalCard sessionId="session" request={permission} />);
    fireEvent.click(screen.getByRole("button", { name: "Deny" }));
    await screen.findByText("Permission expired");
    expect(screen.queryByText("Denied")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Deny" })).toBeEnabled();
  });
});

it("shows only provider-reported subagent IDs/status without guessed progress or task links", () => {
  const view = render(<SubagentCard agent={{ id: "provider-agent-42", name: "Research", status: "waiting-on-provider", task: "Read the spec", message: "", toolCallId: "call-4" }} />);
  expect(screen.getByText("provider-agent-42")).toBeInTheDocument();
  expect(screen.getByText("waiting-on-provider")).toBeInTheDocument();
  expect(screen.queryByRole("link")).not.toBeInTheDocument();
  expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
  expect(view.container.querySelector("a")).toBeNull();
});
