import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { KnowledgeFile } from "@/mygo";

vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", async () => (await import("./testing")).mygoMock);

import { TOOL_IDS, emptyToolSettings, initialForm, type FormState } from "./botForm";
import { KnowledgeTab } from "./KnowledgeTab";
import { bridge, emit, resetBridge } from "./testing";
import { CHUNK_BYTES, MAX_FILE_BYTES } from "./upload";

const file = (id: string, over: Partial<KnowledgeFile> = {}): KnowledgeFile => ({ id, name: `${id}.md`, mime: "text/markdown", size: 2048, botId: "b1", status: "ready", chunks: 3, ...over });

const withSearch = (): FormState => ({ ...initialForm(), tools: { [TOOL_IDS.knowledge]: emptyToolSettings() } });

function renderTab(form: FormState = withSearch(), dispatch = vi.fn(), botId: string | null = "b1") {
  render(<KnowledgeTab botId={botId} form={form} dispatch={dispatch} />);
  return dispatch;
}

const choose = (...files: File[]) => fireEvent.change(screen.getByLabelText("Choose knowledge files"), { target: { files } });

const decodedLength = (base64: string) => atob(base64).length;

beforeEach(() => {
  resetBridge();
  bridge.chat.beginAttachment.mockResolvedValue({ id: "up1" });
  bridge.chat.attachmentChunk.mockResolvedValue(undefined);
  bridge.chat.commitAttachment.mockResolvedValue({ id: "att1", name: "notes.md", mime: "text/markdown", size: 0 });
  bridge.chat.addKnowledge.mockResolvedValue(file("att1"));
  bridge.chat.discardAttachment.mockResolvedValue(undefined);
});
afterEach(cleanup);

describe("knowledge files", () => {
  it("asks to save a new bot first", () => {
    renderTab(withSearch(), vi.fn(), null);
    expect(screen.getByText(/Save the bot first/)).toBeInTheDocument();
    expect(bridge.chat.knowledgeFiles).not.toHaveBeenCalled();
  });

  it("lists files with status, size and the daemon's error text verbatim", async () => {
    bridge.chat.knowledgeFiles.mockResolvedValue([
      file("guide"),
      file("broken", { status: "error", chunks: 0, error: "indexing broken.md: disk is full" }),
      file("big", { chunks: 2000, error: "Only the first 2000 passages were indexed." }),
    ]);
    renderTab();
    expect(await screen.findByText("guide.md")).toBeInTheDocument();
    expect(bridge.chat.knowledgeFiles).toHaveBeenCalledWith("b1");
    expect(screen.getByText("Failed")).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("indexing broken.md: disk is full");
    expect(screen.getByText("Only the first 2000 passages were indexed.")).toBeInTheDocument();
    expect(screen.getByText("3 passages")).toBeInTheDocument();
    expect(screen.getAllByText("2 KB")).toHaveLength(3);
  });

  it("keeps polling while a file is being indexed", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      bridge.chat.knowledgeFiles.mockResolvedValueOnce([file("slow", { status: "indexing", chunks: 0 })]).mockResolvedValue([file("slow")]);
      renderTab();
      expect(await screen.findByText("Indexing")).toBeInTheDocument();
      await vi.advanceTimersByTimeAsync(1600);
      expect(await screen.findByText("Ready")).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  it("reloads when the daemon reports a change", async () => {
    bridge.chat.knowledgeFiles.mockResolvedValueOnce([]).mockResolvedValue([file("new")]);
    renderTab();
    await screen.findByText("No files yet.");
    emit("chatMemory", { scope: "bot:b1" });
    expect(await screen.findByText("new.md")).toBeInTheDocument();
  });

  it("removes a file only after confirmation", async () => {
    bridge.chat.knowledgeFiles.mockResolvedValue([file("guide")]);
    bridge.chat.removeKnowledge.mockResolvedValue(undefined);
    renderTab();
    fireEvent.click(await screen.findByRole("button", { name: "Remove guide.md" }));
    expect(bridge.chat.removeKnowledge).not.toHaveBeenCalled();
    fireEvent.click(await screen.findByRole("button", { name: "Remove file" }));
    await waitFor(() => expect(bridge.chat.removeKnowledge).toHaveBeenCalledWith({ botId: "b1", attachmentId: "guide" }));
  });

  it("shows a refused removal inside the dialog", async () => {
    bridge.chat.knowledgeFiles.mockResolvedValue([file("guide")]);
    bridge.chat.removeKnowledge.mockRejectedValue(new Error("internal: database is locked"));
    renderTab();
    fireEvent.click(await screen.findByRole("button", { name: "Remove guide.md" }));
    fireEvent.click(await screen.findByRole("button", { name: "Remove file" }));
    expect(await screen.findByText("database is locked")).toBeInTheDocument();
  });

  it("points at the Knowledge search tool when it is off", async () => {
    const dispatch = renderTab(initialForm());
    expect(await screen.findByText(/Needs the Knowledge search tool/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Turn on" }));
    expect(dispatch).toHaveBeenCalledWith({ type: "tool", id: "knowledge_search", settings: emptyToolSettings() });
  });
});

describe("uploading", () => {
  it("sends 1 MiB base64 chunks in order, commits, then attaches the file to the bot", async () => {
    const size = CHUNK_BYTES * 2 + 123;
    const bytes = new Uint8Array(size).map((_, i) => i % 251);
    renderTab();
    await screen.findByText("No files yet.");
    choose(new File([bytes], "notes.md", { type: "text/markdown" }));
    await waitFor(() => expect(bridge.chat.addKnowledge).toHaveBeenCalledTimes(1));
    expect(bridge.chat.beginAttachment).toHaveBeenCalledWith({ name: "notes.md", size });
    const chunks = bridge.chat.attachmentChunk.mock.calls.map((c) => c[0] as { id: string; seq: number; dataBase64: string });
    expect(chunks.map((c) => c.seq)).toEqual([0, 1, 2]);
    expect(chunks.every((c) => c.id === "up1")).toBe(true);
    expect(chunks.map((c) => decodedLength(c.dataBase64))).toEqual([CHUNK_BYTES, CHUNK_BYTES, 123]);
    const joined = chunks.map((c) => atob(c.dataBase64)).join("");
    expect(joined.charCodeAt(0)).toBe(0);
    expect(joined.charCodeAt(size - 1)).toBe((size - 1) % 251);
    expect(bridge.chat.commitAttachment).toHaveBeenCalledWith("up1");
    expect(bridge.chat.addKnowledge).toHaveBeenCalledWith({ botId: "b1", attachmentId: "att1" });
    expect(bridge.chat.discardAttachment).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.queryByRole("list", { name: "Uploads" })).toBeNull());
    expect(bridge.chat.knowledgeFiles.mock.calls.length).toBeGreaterThan(1);
  });

  it("uploads a small file in a single chunk", async () => {
    renderTab();
    await screen.findByText("No files yet.");
    choose(new File(["hello"], "hello.txt", { type: "text/plain" }));
    await waitFor(() => expect(bridge.chat.addKnowledge).toHaveBeenCalled());
    expect(bridge.chat.attachmentChunk).toHaveBeenCalledTimes(1);
    expect(atob(bridge.chat.attachmentChunk.mock.calls[0]![0].dataBase64)).toBe("hello");
  });

  it("shows the daemon's indexing refusal verbatim and discards the staged upload", async () => {
    bridge.chat.addKnowledge.mockRejectedValue(new Error("bad_request: \"scan.pdf\" is not a text file, so it cannot be indexed as knowledge"));
    renderTab();
    await screen.findByText("No files yet.");
    choose(new File(["%PDF"], "scan.pdf", { type: "application/pdf" }));
    expect(await screen.findByText("\"scan.pdf\" is not a text file, so it cannot be indexed as knowledge")).toBeInTheDocument();
    expect(bridge.chat.discardAttachment).toHaveBeenCalledWith("up1");
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(screen.queryByText(/is not a text file/)).toBeNull();
  });

  it("stops at a failed chunk, reports it and discards the upload", async () => {
    bridge.chat.attachmentChunk.mockResolvedValueOnce(undefined).mockRejectedValueOnce(new Error("internal: frame too large"));
    renderTab();
    await screen.findByText("No files yet.");
    choose(new File([new Uint8Array(CHUNK_BYTES * 3)], "big.md"));
    expect(await screen.findByText("frame too large")).toBeInTheDocument();
    expect(bridge.chat.attachmentChunk).toHaveBeenCalledTimes(2);
    expect(bridge.chat.commitAttachment).not.toHaveBeenCalled();
    expect(bridge.chat.addKnowledge).not.toHaveBeenCalled();
    expect(bridge.chat.discardAttachment).toHaveBeenCalledWith("up1");
  });

  it("does not transfer a file over the size limit", async () => {
    renderTab();
    await screen.findByText("No files yet.");
    choose(new File([new Uint8Array(MAX_FILE_BYTES + 1)], "huge.md"));
    expect(await screen.findByText("huge.md is larger than 20 MiB, the most a file may be.")).toBeInTheDocument();
    expect(bridge.chat.beginAttachment).not.toHaveBeenCalled();
  });

  it("uploads several files one after another and keeps going after a failure", async () => {
    bridge.chat.beginAttachment.mockResolvedValueOnce({ id: "u1" }).mockResolvedValueOnce({ id: "u2" });
    bridge.chat.commitAttachment.mockRejectedValueOnce(new Error("bad_request: empty file")).mockResolvedValue({ id: "att2", name: "b.md", mime: "text/markdown", size: 1 });
    renderTab();
    await screen.findByText("No files yet.");
    choose(new File(["a"], "a.md"), new File(["b"], "b.md"));
    expect(await screen.findByText("empty file")).toBeInTheDocument();
    await waitFor(() => expect(bridge.chat.addKnowledge).toHaveBeenCalledWith({ botId: "b1", attachmentId: "att2" }));
    expect(bridge.chat.discardAttachment).toHaveBeenCalledWith("u1");
  });
});
