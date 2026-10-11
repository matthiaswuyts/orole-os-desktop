import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Event as AgentEvent, Session, TaskFilePreview } from "@/mygo";
import type { ImageStatusData } from "@/lib/taskChatTypes";

const api = vi.hoisted(() => ({ artifact: vi.fn(), taskFile: vi.fn(), diff: vi.fn(), openExternal: vi.fn() }));
vi.mock("@/mygo", () => ({ Agents: api, Shell: { openExternal: api.openExternal } }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("./TaskInteractions", () => ({ TaskApprovalCard: () => null }));
import { groupEvents } from "@/lib/taskEvents";
import { Markdown } from "@/components/Markdown";
import { highlightCode, SyntaxHighlighter } from "@/components/SyntaxHighlighter";
import { FileCard, GuardrailNotice, SourcesCard } from "./FeedItems";
import { ImageGenerationCard, TaskImage } from "./TaskImages";
import { TaskFilePreviewContent } from "./TaskFiles";
import { TaskCanvas } from "./TaskCanvas";
import { TaskFileTree } from "./TaskFileTree";

beforeEach(() => {
  vi.clearAllMocks();
  api.diff.mockResolvedValue({ diff: { files: [], truncated: false }, truncated: false, error: "" });
});
afterEach(cleanup);

const session = (id = "rich-session") => ({ id, mode: "structured", machine: "local", live: false, updatedAt: "now" }) as Session;
const file = { id: "f1", path: "src/report.ts", name: "report.ts", mime: "text/plain", status: "reported", summary: "Report implementation" };
const preview = (patch: Partial<TaskFilePreview> = {}): TaskFilePreview => ({ name: "report.ts", path: "src/report.ts", mime: "text/plain", size: 12, text: "const x = 1;", truncated: false, ...patch });

describe("safe rich rendering", () => {
  it("uses the installed grammar without changing the supplied code", async () => {
    const code = "const answer = 42;";
    render(<SyntaxHighlighter code={code} language="typescript" />);
    const block = screen.getByLabelText("typescript code");
    await waitFor(() => expect(block.querySelector("span")).not.toBeNull());
    expect(block.textContent).toBe(code);
  });

  it("keeps unknown fenced languages inert and disables HTML and remote images", async () => {
    const hostile = '<img src="https://evil.test/x" onerror="alert(1)">';
    expect(await highlightCode(hostile, "not-a-language")).toEqual([hostile]);
    const { container } = render(<Markdown>{`\`\`\`not-a-language\n${hostile}\n\`\`\`\n\n![remote](https://evil.test/image.png)\n<script>alert(1)</script>`}</Markdown>);
    expect(screen.getByLabelText("not-a-language code")).toHaveTextContent(hostile);
    expect(container.querySelector("img, script, iframe")).toBeNull();
    expect(screen.getByRole("button", { name: "Copy code" })).toBeInTheDocument();
  });

  it("only renders supplied http(s) sources and opens them through the shell", async () => {
    render(<SourcesCard sources={[
      { id: "s1", title: "Reference", url: "https://example.com/ref", snippet: "Exact supplied excerpt" },
      { id: "s2", title: "Unsafe", url: "javascript:alert(1)", snippet: "" },
      { id: "s3", title: "Local", url: "file:///secret", snippet: "" },
    ]} />);
    fireEvent.click(screen.getByRole("button", { name: /Sources/ }));
    expect(screen.getAllByRole("link")).toHaveLength(1);
    expect(screen.queryByText("Unsafe")).toBeNull();
    expect(screen.getByText("Exact supplied excerpt")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("link", { name: "Reference" }));
    await waitFor(() => expect(api.openExternal).toHaveBeenCalledWith("https://example.com/ref"));
    expect(document.querySelector("img")).toBeNull();
  });

  it("does not promote a provider MIME claim over the returned artifact MIME", async () => {
    api.artifact.mockResolvedValue({ mime: "image/svg+xml", data: "PHN2Zz4=" });
    render(<TaskImage sessionId="mime-test" image={{ id: "i1", path: "unsafe.svg", mime: "image/png", prompt: "claimed raster" }} />);
    expect(await screen.findByRole("alert")).toHaveTextContent("Unsupported image type image/svg+xml");
    expect(screen.queryByRole("img")).toBeNull();
  });

  it("renders generation stages only when supplied, without fabricated progress or resolution", () => {
    const generation: ImageStatusData = { id: "g1", status: "running", prompt: "A real requested image", error: "" };
    const view = render(<ImageGenerationCard generation={generation} sessionId="stage-test" />);
    expect(screen.getByRole("status")).toHaveTextContent("Generating image");
    expect(screen.queryByText(/refin|1024|%/i)).toBeNull();
    expect(api.artifact).not.toHaveBeenCalled();
    view.rerender(<ImageGenerationCard generation={{ ...generation, status: "complete" }} sessionId="stage-test" />);
    expect(screen.getByRole("status")).toHaveTextContent("Generation complete");
    expect(screen.getByText("No image artifact was supplied.")).toBeInTheDocument();
    expect(screen.queryByRole("img")).toBeNull();
  });

  it("presents a policy notice without invented alternatives or retry authority", () => {
    render(<GuardrailNotice notice={{ id: "g", kind: "refusal", message: "Provider refused this request.", source: "Provider" }} />);
    expect(screen.getByRole("status")).toHaveTextContent("Provider refused this request.");
    expect(screen.queryByRole("button")).toBeNull();
  });
});

describe("task file previews", () => {
  it("uses scoped file metadata and never interprets HTML preview text", async () => {
    api.taskFile.mockResolvedValue(preview({ name: "report.html", mime: "text/html", size: 64, text: "<script>alert(1)</script>" }));
    const { container } = render(<TaskFilePreviewContent sessionId="files-test" path="src/report.html" />);
    expect(await screen.findByText(/64 bytes/)).toBeInTheDocument();
    expect(api.taskFile).toHaveBeenCalledWith("files-test", "src/report.html");
    expect(container.querySelector("script, iframe")).toBeNull();
    expect(screen.getByLabelText("html code")).toHaveTextContent("<script>alert(1)</script>");
    expect(screen.getByRole("button", { name: "Download" })).toBeInTheDocument();
  });

  it("does not offer a complete-file download for truncated data", async () => {
    api.taskFile.mockResolvedValue(preview({ truncated: true }));
    render(<TaskFilePreviewContent sessionId="files-test" path={file.path} />);
    expect(await screen.findByText(/Preview truncated/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Download" })).toBeNull();
  });

  it("ignores an old file request completing after navigation", async () => {
    let resolveOld!: (value: TaskFilePreview) => void;
    api.taskFile.mockReturnValueOnce(new Promise<TaskFilePreview>((resolve) => { resolveOld = resolve; }));
    api.taskFile.mockResolvedValueOnce(preview({ name: "new.txt", path: "new.txt", text: "new content" }));
    const view = render(<TaskFilePreviewContent sessionId="files-test" path="old.txt" />);
    view.rerender(<TaskFilePreviewContent sessionId="files-test" path="new.txt" />);
    await screen.findByText("new content");
    await act(async () => { resolveOld(preview({ path: "old.txt", text: "stale content" })); });
    expect(screen.queryByText("stale content")).toBeNull();
    expect(screen.getByText("new content")).toBeInTheDocument();
  });

  it("surfaces a denied file and retries the same scoped request explicitly", async () => {
    api.taskFile.mockRejectedValueOnce(new Error("Outside the task root"));
    api.taskFile.mockResolvedValueOnce(preview());
    render(<TaskFilePreviewContent sessionId="files-test" path={file.path} />);
    expect(await screen.findByRole("alert")).toHaveTextContent("Outside the task root");
    fireEvent.click(screen.getByRole("button", { name: "Retry file" }));
    expect(await screen.findByRole("button", { name: "Download" })).toBeInTheDocument();
    expect(api.taskFile).toHaveBeenNthCalledWith(2, "files-test", file.path);
  });
});

describe("task canvas", () => {
  it("opens real file content from the feed and restores focus on close", async () => {
    api.taskFile.mockResolvedValue(preview());
    render(<TaskCanvas session={session()} items={[]}><FileCard file={file} sessionId="rich-session" /></TaskCanvas>);
    const trigger = screen.getByRole("button", { name: "Preview" });
    trigger.focus();
    fireEvent.click(trigger);
    const canvas = screen.getByRole("region", { name: "Task canvas" });
    expect(await within(canvas).findByText(/12 bytes/)).toBeInTheDocument();
    expect(api.taskFile).toHaveBeenCalledWith("rich-session", file.path);
    fireEvent.click(screen.getByRole("button", { name: "Close canvas" }));
    expect(screen.queryByRole("region", { name: "Task canvas" })).toBeNull();
    expect(trigger).toHaveFocus();
  });

  it("clears the visible canvas when the session changes", () => {
    const view = render(<TaskCanvas session={session("one")} items={[]}><p>Conversation one</p></TaskCanvas>);
    fireEvent.click(screen.getByRole("button", { name: "Task files" }));
    expect(screen.getByRole("region", { name: "Task canvas" })).toBeInTheDocument();
    view.rerender(<TaskCanvas session={session("two")} items={[]}><p>Conversation two</p></TaskCanvas>);
    expect(screen.queryByRole("region", { name: "Task canvas" })).toBeNull();
  });

  it("lists only reported files and fetched changed files", async () => {
    api.diff.mockResolvedValue({ error: "", truncated: false, diff: { files: [{ path: "changed.txt", oldPath: "", status: "modified", additions: 1, deletions: 0, binary: false, hunks: [] }], truncated: false } });
    const events = [{ id: 1, sessionId: "rich-session", at: "2026-10-08T12:00:00Z", kind: "file", text: "", data: file }] as AgentEvent[];
    render(<TaskCanvas session={session()} items={groupEvents(events)}><p>Conversation</p></TaskCanvas>);
    fireEvent.click(screen.getByRole("button", { name: /Task files/ }));
    const tree = screen.getByRole("navigation", { name: "Task files" });
    expect(await within(tree).findByRole("button", { name: /changed.txt/ })).toBeInTheDocument();
    expect(within(tree).getByRole("button", { name: /report.ts/ })).toBeInTheDocument();
    expect(within(tree).getAllByRole("button")).toHaveLength(2);
  });

  it("deduplicates tree rows and returns the original reported path", () => {
    const onOpen = vi.fn();
    render(<TaskFileTree files={[{ path: "src\\one.ts" }, { path: "src\\one.ts" }, { path: "src/two.ts" }]} onOpen={onOpen} />);
    expect(screen.getAllByRole("button")).toHaveLength(2);
    fireEvent.click(screen.getByRole("button", { name: "one.ts" }));
    expect(onOpen).toHaveBeenCalledWith("src\\one.ts");
  });
});
