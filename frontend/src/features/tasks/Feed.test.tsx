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
const ev = (kind: string, text = "", data: unknown = null, sec = 0): AgentEvent => ({ id: ++seq, sessionId: "s1", at: new Date(Date.UTC(2026, 9, 8, 12, 0, sec)).toISOString(), kind, text, data });
/** The run's own disclosure trigger, not a step's. */
const run = () => document.querySelector<HTMLElement>('[data-slot="run-activity"] > [data-slot="collapsible"] > [data-slot="collapsible-trigger"]')!;
const session = (patch: Partial<Session> = {}): Session => ({ id: "s1", live: true, status: "idle", activity: "", pending: [], ...patch }) as Session;
const show = (events: AgentEvent[], s = session()) => render(<Feed session={s} events={events} loading={false} loadingMore={false} error={null} hasMore={false} onLoadMore={() => Promise.resolve(true)} />);

describe("Feed", () => {
  it("folds a tool call and its result into one step of the run, readable without raw JSON or status clutter", () => {
    const { container } = show([
      ev("prompt", "look it up"),
      ev("tool", "", { id: "t1", name: "search", server: "docs", status: "running", input: { query: "hooks" } }),
      ev("tool_result", "", { id: "t1", name: "search", server: "docs", status: "ok", output: "3 results" }),
      ev("tool", "", { id: "t2", name: "web_search", status: "ok", input: { action: null, query: "" } }),
      ev("tool", "", { id: "t3", name: "shell", status: "ok", input: { command: `"C:\\Program Files\\PowerShell\\7\\pwsh.exe" -Command 'npm test'`, cwd: "C:\\w" } }),
      ev("message", "Found it."),
    ]);
    expect(screen.getByText("Found it.")).toBeInTheDocument();
    expect(screen.queryByText("3 results")).toBeNull();
    fireEvent.click(run());
    const steps = within(screen.getByRole("list", { name: "Activity" })).getAllByRole("listitem");
    expect(steps).toHaveLength(3);
    expect(steps[0]).toHaveTextContent("Used");
    expect(steps[0]).toHaveTextContent("docs · Search");
    expect(steps[1]).toHaveTextContent("Searched the web");
    expect(steps[1]).not.toHaveTextContent("action");
    expect(steps[2]).toHaveTextContent(/Ran\s*npm test/);
    expect(steps[2]).not.toHaveTextContent("pwsh");
    expect(container).not.toHaveTextContent("Done");
    expect(container.querySelectorAll("time")).toHaveLength(container.querySelectorAll('[data-slot="day-separator"] time').length);
    // The technical record is one disclosure away.
    fireEvent.click(within(steps[0]!).getByRole("button"));
    const details = screen.getByTestId("tool-details");
    expect(details).toHaveAttribute("data-status", "ok");
    expect(details).toHaveTextContent("search");
    expect(screen.getByLabelText("Tool output")).toHaveTextContent("3 results");
    expect(screen.getByLabelText("Tool input")).toHaveTextContent('"query": "hooks"');
  });

  it("shows a failed tool as an explicit error and keeps the time in a hover title only", () => {
    show([ev("prompt", "go"), ev("tool", "", { id: "t1", name: "shell", status: "error", input: { command: "npm test" }, output: "exit code 1" }), ev("message", "It failed.")]);
    fireEvent.click(run());
    const step = within(screen.getByRole("list", { name: "Activity" })).getByRole("listitem");
    expect(step).toHaveAttribute("data-failed", "true");
    expect(step).toHaveTextContent("Failed");
    expect(step.getAttribute("title")).toBeTruthy();
  });

  it("folds thoughts into the run as commentary labelled by their title, with the full text one click away", () => {
    show([ev("prompt", "plan"), ev("reasoning", "**Planning the change**\n\nThen run the tests carefully.", { id: "r1" }), ev("message", "Ready.")]);
    expect(screen.queryByText("Planning the change")).toBeNull();
    fireEvent.click(run());
    const step = screen.getByText("Planning the change");
    expect(step.closest("li")).not.toHaveTextContent("Thought");
    expect(screen.queryByText("Then run the tests carefully.")).toBeNull();
    fireEvent.click(within(step.closest("li")!).getByRole("button"));
    expect(screen.getByText("Then run the tests carefully.")).toBeInTheDocument();
  });

  it("keeps the final message outside the disclosure and labels the run by its real duration", () => {
    show([
      ev("prompt", "go", null, 0),
      ev("tool", "", { id: "t1", name: "Read", status: "ok", input: { file_path: "a.go" } }, 10),
      ev("message", "Interim note.", null, 20),
      ev("tool", "", { id: "t2", name: "Read", status: "ok", input: { file_path: "b.go" } }, 30),
      ev("message", "All done.", null, 133),
      ev("turn_done", "All done.", { ok: true }, 133),
    ]);
    expect(run()).toHaveTextContent("Worked for");
    expect(run()).toHaveTextContent("2m 13s");
    expect(screen.getByText("All done.")).toBeInTheDocument();
    expect(screen.queryByText("Interim note.")).toBeNull();
    expect(screen.queryByRole("separator", { name: /Turn complete/ })).toBeNull();
    fireEvent.click(run());
    expect(screen.getAllByText("Interim note.").length).toBeGreaterThan(0);
  });

  it("omits the duration when the turn has no usable timestamps", () => {
    show([ev("prompt", "go"), ev("tool", "", { id: "t1", name: "Read", status: "ok", input: { file_path: "a.go" } }), ev("message", "ok"), ev("turn_done", "ok", { ok: true })]);
    expect(run()).toHaveTextContent("Completed");
    expect(run()).not.toHaveTextContent(/\d+s/);
  });

  it("keeps a run's open state across updates and completion", () => {
    const events = [ev("prompt", "go"), ev("tool", "", { id: "t1", name: "Read", status: "ok", input: { file_path: "a.go" } })];
    const props = { loading: false, loadingMore: false, error: null, hasMore: false, onLoadMore: () => Promise.resolve(true) };
    const view = render(<Feed {...props} session={session({ status: "working", activity: "Reading" })} events={events} />);
    expect(run()).toHaveTextContent("Reading");
    expect(screen.queryByRole("list", { name: "Activity" })).toBeNull();
    fireEvent.click(run());
    expect(screen.getByRole("list", { name: "Activity" })).toBeInTheDocument();
    view.rerender(<Feed {...props} session={session({ status: "working", activity: "Reading" })} events={[...events, ev("tool", "", { id: "t2", name: "Read", status: "ok", input: { file_path: "b.go" } })]} />);
    expect(within(screen.getByRole("list", { name: "Activity" })).getAllByRole("listitem")).toHaveLength(2);
    view.rerender(<Feed {...props} session={session()} events={[...events, ev("message", "Done."), ev("turn_done", "Done.", { ok: true })]} />);
    expect(run()).toHaveTextContent("Completed");
    expect(screen.getByRole("list", { name: "Activity" })).toBeInTheDocument();
  });

  it("renders data-less legacy events without crashing", () => {
    show([ev("prompt", "fix it"), ev("tool", "Bash: npm test"), ev("tool_result", "passed", { error: false }), ev("message", "All good."), ev("turn_done", "All good.")]);
    expect(screen.getByText("fix it")).toBeInTheDocument();
    expect(screen.getByText("All good.")).toBeInTheDocument();
    fireEvent.click(run());
    const step = within(screen.getByRole("list", { name: "Activity" })).getByRole("listitem");
    expect(step).toHaveTextContent(/Ran\s*npm test/);
    fireEvent.click(within(step).getByRole("button"));
    expect(screen.getByTestId("tool-details")).toHaveAttribute("data-status", "ok");
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

  it("shows a pending approval as a compact card outside the collapsed run", () => {
    const input = '{"kind":"command","command":"rm x","cwd":"C:\\\\work"}';
    const { container } = show([
      ev("prompt", "clean up"),
      ev("tool", "", { id: "t1", name: "shell", status: "running", input: { command: "rm x" } }),
      ev("permission", "shell", { id: "p1", tool: "shell", summary: "rm x", input: JSON.parse(input), inputJSON: input, at: "2026-10-08T12:00:00Z" }),
    ], session({ status: "needs-you", pending: [{ id: "p1" } as Session["pending"][number]] }));
    expect(run()).toHaveTextContent("Needs your input");
    expect(screen.queryByRole("list", { name: "Activity" })).toBeNull();
    expect(screen.getByText("Run a command")).toBeInTheDocument();
    expect(screen.getByLabelText("Command")).toHaveTextContent("rm x");
    expect(screen.queryByText(/Tool permission|Permission request history/)).toBeNull();
    expect(container.textContent).not.toContain('"kind"');
    expect(screen.getByRole("button", { name: "Allow once" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Deny" })).toBeEnabled();
  });

  it("folds a decided approval into the run as a one-line receipt", () => {
    show([
      ev("prompt", "clean up"),
      ev("permission", "shell", { id: "p1", tool: "shell", summary: "ls", input: { command: "ls" }, inputJSON: '{"command":"ls"}' }),
      ev("decision", "", { id: "p1", allow: true, reason: "" }),
      ev("message", "Listed."),
    ]);
    expect(screen.queryByRole("button", { name: "Allow once" })).toBeNull();
    fireEvent.click(run());
    expect(within(screen.getByRole("list", { name: "Activity" })).getByRole("listitem")).toHaveTextContent("Allowed");
  });

  it("opens a collapsed run to reveal a search match inside its interim messages", () => {
    const { container } = show([
      ev("prompt", "go"),
      ev("message", "Checking the needle first."),
      ev("tool", "", { id: "t1", name: "Read", status: "ok", input: { file_path: "a.go" } }),
      ev("message", "Final answer."),
    ]);
    expect(screen.queryByText("Checking the needle first.")).toBeNull();
    fireEvent.keyDown(screen.getByRole("log"), { key: "f", ctrlKey: true });
    fireEvent.change(screen.getByRole("searchbox"), { target: { value: "needle" } });
    expect(container.querySelectorAll("[data-search-hit] mark")).toHaveLength(1);
    expect(screen.getByRole("list", { name: "Activity" })).toBeInTheDocument();
  });

  it("holds the place of a running image generation with the ImageGeneration frame", () => {
    show([ev("prompt", "draw"), ev("image_status", "", { id: "g1", status: "running", prompt: "a calm lake" })], session({ status: "working", activity: "Working" }));
    expect(screen.getByText("Generating")).toBeInTheDocument();
    expect(screen.getByText("a calm lake")).toBeInTheDocument();
    expect(api.artifact).not.toHaveBeenCalled();
  });
});

describe("Feed navigation", () => {
  it("steps through highlighted matches without filtering history or hiding paging", async () => {
    const older = ev("message", "Older needle");
    const loaded = [ev("prompt", "Find the needle"), ev("message", "Another needle, then needle.")];
    const load = vi.fn().mockResolvedValue(true);
    const props = { session: session(), loading: false, loadingMore: false, error: null, hasMore: true, onLoadMore: load };
    const view = render(<Feed {...props} events={loaded} />);
    fireEvent.keyDown(screen.getByRole("log"), { key: "f", ctrlKey: true });
    const input = screen.getByRole("searchbox", { name: "Find in conversation" });
    fireEvent.change(input, { target: { value: "needle" } });
    expect(screen.getByRole("status")).toHaveTextContent("1/3");
    fireEvent.keyDown(input, { key: "Enter" });
    expect(screen.getByRole("status")).toHaveTextContent("2/3");
    expect(view.container.querySelectorAll('[data-search-hit] mark')).toHaveLength(3);
    const active = view.container.querySelector('[data-search-hit][aria-current="true"]')?.getAttribute("data-search-hit");
    fireEvent.click(screen.getByRole("button", { name: "Load earlier messages" }));
    expect(load).toHaveBeenCalledTimes(1);
    view.rerender(<Feed {...props} events={[older, ...loaded]} />);
    expect(screen.getByRole("status")).toHaveTextContent("3/4");
    expect(view.container.querySelector('[data-search-hit][aria-current="true"]')).toHaveAttribute("data-search-hit", active);
    expect(screen.getByText("Older needle")).toBeInTheDocument();
    fireEvent.keyDown(input, { key: "Escape" });
    expect(screen.queryByRole("searchbox")).toBeNull();
    expect(view.container.querySelectorAll("[data-search-hit]")).toHaveLength(0);
  });

  it("resets search on session switch and supports scoped keyboard find", () => {
    const props = { loading: false, loadingMore: false, error: null, hasMore: false, onLoadMore: vi.fn().mockResolvedValue(false) };
    const view = render(<Feed {...props} session={session()} events={[ev("message", "needle")]} />);
    expect(screen.queryByRole("searchbox")).toBeNull();
    const log = screen.getByRole("log");
    fireEvent.keyDown(log, { key: "f", ctrlKey: true });
    const input = screen.getByRole("searchbox");
    expect(input).toHaveFocus();
    fireEvent.change(input, { target: { value: "needle" } });
    view.rerender(<Feed {...props} session={session({ id: "s2" })} events={[]} />);
    expect(screen.queryByRole("searchbox")).toBeNull();
  });

  it("distinguishes failed loading from empty history and retries only the supplied operation", () => {
    const retry = vi.fn();
    render(<Feed session={session()} events={[]} loading={false} loadingMore={false} error="Transcript unavailable" hasMore={false} onLoadMore={vi.fn()} onRetry={retry} />);
    expect(screen.getByRole("alert")).toHaveTextContent("Transcript unavailable");
    expect(screen.queryByText("No output has been recorded yet.")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Retry loading conversation" }));
    expect(retry).toHaveBeenCalledTimes(1);
  });
});

describe("pending inputs outside the loaded page", () => {
  it("keeps a real pending question reachable and deduplicates it when its event loads", () => {
    const request = {
      id: "outside-page", at: "2026-10-08T11:00:00Z",
      questions: [{ id: "choice", header: "", prompt: "Choose the target", options: [{ id: "a", label: "Project A", description: "" }], multiple: false, allowText: false, secret: false }],
    };
    const s = session({ questions: [request] });
    const props = { session: s, loading: false, loadingMore: false, error: null, hasMore: true, onLoadMore: vi.fn().mockResolvedValue(true) };
    const view = render(<Feed {...props} events={[]} />);
    expect(screen.getAllByText("Choose the target")).toHaveLength(1);
    view.rerender(<Feed {...props} events={[ev("question", "", request)]} />);
    expect(screen.getAllByText("Choose the target")).toHaveLength(1);
  });
});
