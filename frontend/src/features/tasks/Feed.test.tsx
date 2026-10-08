import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Event as AgentEvent, Session } from "@/mygo";

const api = vi.hoisted(() => ({ artifact: vi.fn(), openExternal: vi.fn() }));
vi.mock("@/mygo", () => ({ Agents: { approve: vi.fn(), get: vi.fn(), artifact: api.artifact }, Shell: { openExternal: api.openExternal }, events: { agentsEvent: { on: () => () => {} } } }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/stores/agents", () => ({ useAgents: { getState: () => ({ upsert: vi.fn() }) } }));
vi.mock("@/stores/boxes", () => ({ useBoxes: vi.fn() }));
import { Feed } from "./Feed";

beforeEach(() => { vi.clearAllMocks(); });
afterEach(cleanup);

let seq = 0;
const ev = (kind: string, text = "", data: unknown = null): AgentEvent => ({ id: ++seq, sessionId: "s1", at: "2026-10-08T12:00:00Z", kind, text, data });
const session = (patch: Partial<Session> = {}): Session => ({ id: "s1", live: true, status: "idle", activity: "", pending: [], ...patch }) as Session;
const show = (events: AgentEvent[], s = session()) => render(<Feed session={s} events={events} loading={false} loadingMore={false} error={null} hasMore={false} onLoadMore={() => Promise.resolve(true)} />);

describe("Feed", () => {
  it("renders one tool card for a call and its result, with server badge, status and expandable input/output", () => {
    show([
      ev("tool", "", { id: "t1", name: "search", server: "docs", status: "running", input: { query: "hooks" } }),
      ev("tool_result", "", { id: "t1", name: "search", server: "docs", status: "ok", output: "3 results" }),
    ]);
    const cards = screen.getAllByTestId("tool-card");
    expect(cards).toHaveLength(1);
    expect(cards[0]).toHaveAttribute("data-status", "ok");
    expect(within(cards[0]!).getByText("docs")).toBeInTheDocument();
    expect(screen.queryByText("3 results")).toBeNull();
    fireEvent.click(within(cards[0]!).getByRole("button"));
    expect(screen.getByLabelText("Tool output")).toHaveTextContent("3 results");
    expect(screen.getByLabelText("Tool input")).toHaveTextContent('"query": "hooks"');
  });

  it("collapses reasoning to a one-line preview and expands to the full text", () => {
    show([ev("reasoning", "Plan the change.\nThen run the tests carefully.", { id: "r1" })]);
    const block = screen.getByTestId("reasoning");
    expect(block).toHaveTextContent("Plan the change. Then run the tests carefully.");
    expect(block.querySelector("p")).toBeNull();
    fireEvent.click(within(block).getByRole("button"));
    expect(block.querySelectorAll("p").length).toBeGreaterThan(0);
  });

  it("renders data-less legacy events without crashing", () => {
    show([ev("prompt", "fix it"), ev("tool", "Bash: npm test"), ev("tool_result", "passed", { error: false }), ev("message", "All good."), ev("turn_done", "All good.")]);
    expect(screen.getByText("fix it")).toBeInTheDocument();
    expect(screen.getByTestId("tool-card")).toHaveAttribute("data-status", "ok");
    expect(screen.getByText("All good.")).toBeInTheDocument();
    expect(screen.getByRole("separator", { name: /Turn complete/ })).toBeInTheDocument();
  });

  it("loads a generated image through Agents.artifact and shows it as a data URL", async () => {
    api.artifact.mockResolvedValue({ mime: "image/png", data: "AAAA" });
    show([ev("image", "a fox", { id: "i1", path: "C:\\scratch\\fox.png", mime: "image/png", prompt: "a red fox" })]);
    const img = await screen.findByRole("img", { name: "a red fox" });
    expect(api.artifact).toHaveBeenCalledWith("s1", "C:\\scratch\\fox.png");
    expect(img).toHaveAttribute("src", "data:image/png;base64,AAAA");
  });

  it("refuses artifact types that are not raster images", async () => {
    api.artifact.mockResolvedValue({ mime: "image/svg+xml", data: "AAAA" });
    show([ev("image", "x", { id: "i2", path: "C:\\scratch\\x.svg", mime: "image/svg+xml", prompt: "svg" })]);
    expect(await screen.findByRole("alert")).toHaveTextContent("Unsupported image type");
    expect(screen.queryByRole("img")).toBeNull();
  });

  it("renders Markdown without raw HTML or remote images, and opens links through the shell", async () => {
    const { container } = show([ev("message", "**bold** <script>alert(1)</script> <img src=x onerror=y> ![remote](https://evil.test/a.png) [docs](https://example.com/a)")]);
    expect(container.querySelector("strong")).toHaveTextContent("bold");
    expect(container.querySelector("script")).toBeNull();
    expect(container.querySelector("img")).toBeNull();
    fireEvent.click(screen.getByRole("link", { name: "docs" }));
    await waitFor(() => expect(api.openExternal).toHaveBeenCalledWith("https://example.com/a"));
  });

  it("shows a live Thinking… indicator only while the session says it is thinking", () => {
    const view = show([], session({ status: "working", activity: "Thinking…" }));
    expect(screen.getByTestId("live-activity")).toHaveTextContent("Thinking…");
    view.rerender(<Feed session={session({ status: "idle" })} events={[]} loading={false} loadingMore={false} error={null} hasMore={false} onLoadMore={() => Promise.resolve(true)} />);
    expect(screen.queryByTestId("live-activity")).toBeNull();
  });

  it("keeps the review checkbox rule on permission cards in the feed", () => {
    const input = '{"command":"rm x"}';
    show([ev("permission", "Bash: rm x", { id: "p1", tool: "Bash", summary: "rm x", input: { command: "rm x" }, inputJSON: input, at: "2026-10-08T12:00:00Z" })], session({ pending: [{ id: "p1" } as Session["pending"][number]] }));
    expect(screen.getByRole("button", { name: "Allow" })).toBeDisabled();
    fireEvent.click(screen.getByRole("checkbox", { name: "I reviewed the complete tool name and arguments." }));
    expect(screen.getByRole("button", { name: "Allow" })).toBeEnabled();
  });
});
