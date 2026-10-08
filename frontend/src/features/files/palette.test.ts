import { beforeEach, describe, expect, it, vi } from "vitest";
import { filesProvider } from "./palette";

const mocks = vi.hoisted(() => ({ query: vi.fn(), openFile: vi.fn(), setLocation: vi.fn(), setView: vi.fn() }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({ Search: { query: mocks.query } }));
vi.mock("@/stores/files", () => ({ openFile: mocks.openFile, useFiles: { getState: () => ({ setLocation: mocks.setLocation }) } }));
vi.mock("@/stores/ui", () => ({ useUI: { getState: () => ({ setView: mocks.setView }) } }));
beforeEach(() => vi.clearAllMocks());

describe("global files palette provider", () => {
  it("opens folders in the browser rather than reading them as files", async () => {
    mocks.query.mockResolvedValue({ results: [{ title: "src", action: { provider: "git", path: "C:\\repo\\src", isDir: "true" } }] });
    const results = await filesProvider.search("src", new AbortController().signal);
    await results[0]!.run();
    expect(mocks.setLocation).toHaveBeenCalledWith("git", "C:\\repo\\src");
    expect(mocks.setView).toHaveBeenCalledWith("files");
    expect(mocks.openFile).not.toHaveBeenCalled();
  });
  it("drops responses for superseded queries", async () => {
    const controller = new AbortController();
    mocks.query.mockImplementation(async () => { controller.abort(); return { results: [{ title: "stale.ts", action: { provider: "local", path: "C:\\work\\stale.ts" } }] }; });
    expect(await filesProvider.search("hello", controller.signal)).toEqual([]);
  });
  it("surfaces provider errors when no results could be read", async () => {
    mocks.query.mockResolvedValue({ results: [], errors: { "files:local": "root unavailable" } });
    await expect(filesProvider.search("hello", new AbortController().signal)).rejects.toThrow("root unavailable");
  });
  it("retains successful hits while displaying another root's failure", async () => {
    const report = vi.fn();
    mocks.query.mockResolvedValue({ results: [{ title: "hello.ts", action: { provider: "local", path: "C:\\work\\hello.ts" } }], errors: { "files:git": "project unavailable" } });
    const results = await filesProvider.search("hello", new AbortController().signal, report);
    expect(results).toHaveLength(1);
    expect(report).toHaveBeenCalledWith("files:git: project unavailable");
  });
});
