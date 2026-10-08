import { beforeEach, describe, expect, it, vi } from "vitest";
import type { FileRead, FilesInfo, FileWriteResult } from "@/mygo";
import { decodeFile, encodeFile, isDirty, joinPath, parentPath, useFiles } from "./files";

const mocks = vi.hoisted(() => ({ read: vi.fn(), write: vi.fn() }));
vi.mock("@/mygo", () => ({ Files: mocks }));
const info = (over: Partial<FilesInfo> = {}): FilesInfo => ({ name: "file.ts", path: "C:\\work\\file.ts", isDir: false, size: 6, modTime: "2026-10-08T12:00:00Z", etag: "etag-1", ...over });
const read = (text = "first\n", over: Partial<FileRead> = {}): FileRead => ({ data: encodeFile({ text, bom: false, lineEnding: "\n" }), offset: 0, size: text.length, truncated: false, info: info(), ...over });
const active = () => useFiles.getState().tabs.find((t) => t.id === useFiles.getState().activeId)!;
const open = async (value = read()) => { mocks.read.mockResolvedValue(value); await useFiles.getState().openFile("local", value.info.path); return active().id; };

beforeEach(() => {
  vi.resetAllMocks();
  useFiles.setState({ tabs: [], activeId: null, provider: "local", directory: "" });
});

describe("editor dirty state", () => {
  it("becomes clean again when text is reverted and preserves edits between tabs", async () => {
    const id = await open();
    expect(isDirty(active())).toBe(false);
    useFiles.getState().edit(id, "changed\n");
    expect(isDirty(active())).toBe(true);
    await open(read("second", { info: info({ path: "C:\\work\\other.ts", name: "other.ts" }) }));
    useFiles.getState().activate(id);
    expect(active().text).toBe("changed\n");
    useFiles.getState().edit(id, "first\n");
    expect(isDirty(active())).toBe(false);
  });
  it("sends the read etag and marks only the saved snapshot clean", async () => {
    const id = await open();
    let finish!: (result: FileWriteResult) => void;
    mocks.write.mockReturnValue(new Promise<FileWriteResult>((resolve) => { finish = resolve; }));
    useFiles.getState().edit(id, "snapshot\n");
    const saving = useFiles.getState().save(id);
    expect(mocks.write).toHaveBeenCalledWith("local", "C:\\work\\file.ts", btoa("snapshot\n"), "etag-1", false, false);
    useFiles.getState().edit(id, "typed while saving\n");
    finish({ conflict: false, info: info({ etag: "etag-2" }) });
    expect(await saving).toBe(true);
    expect(active()).toMatchObject({ text: "typed while saving\n", savedText: "snapshot\n", etag: "etag-2", saving: false });
    expect(isDirty(active())).toBe(true);
  });
  it("does not send duplicate saves or close a pending save", async () => {
    const id = await open();
    let finish!: (result: FileWriteResult) => void;
    mocks.write.mockReturnValue(new Promise<FileWriteResult>((resolve) => { finish = resolve; }));
    useFiles.getState().edit(id, "changed");
    const pending = useFiles.getState().save(id);
    expect(await useFiles.getState().save(id)).toBe(false);
    expect(useFiles.getState().close(id, true)).toBe(false);
    expect(mocks.write).toHaveBeenCalledTimes(1);
    finish({ conflict: false, info: info() }); await pending;
  });
  it("requires explicit discard before closing dirty tabs", async () => {
    const id = await open();
    useFiles.getState().edit(id, "unsaved");
    expect(useFiles.getState().close(id)).toBe(false);
    expect(useFiles.getState().tabs).toHaveLength(1);
    expect(useFiles.getState().close(id, true)).toBe(true);
    expect(useFiles.getState().tabs).toHaveLength(0);
  });
  it("reuses renamed tabs while allowing a new file at the old path", async () => {
    const id = await open();
    useFiles.getState().edit(id, "unsaved");
    const moved = info({ path: "C:\\work\\renamed.ts", name: "renamed.ts" });
    useFiles.getState().renamed("local", "C:\\work\\file.ts", moved);
    await useFiles.getState().openFile("local", moved.path);
    expect(useFiles.getState().tabs).toHaveLength(1);
    expect(active()).toMatchObject({ id, text: "unsaved", path: moved.path });
    await open();
    expect(useFiles.getState().tabs).toHaveLength(2);
    expect(active().id).not.toBe(id);
  });
});

describe("optimistic conflicts", () => {
  it("keeps edits and the original etag on conflict; force is only explicit overwrite", async () => {
    const id = await open();
    useFiles.getState().edit(id, "mine");
    mocks.write.mockResolvedValueOnce({ conflict: true, current: info({ etag: "external" }) });
    expect(await useFiles.getState().save(id)).toBe(false);
    expect(active()).toMatchObject({ text: "mine", savedText: "first\n", etag: "etag-1", conflict: { conflict: true } });
    expect(isDirty(active())).toBe(true);
    mocks.write.mockResolvedValueOnce({ conflict: false, info: info({ etag: "forced" }) });
    expect(await useFiles.getState().save(id, true)).toBe(true);
    expect(mocks.write).toHaveBeenLastCalledWith("local", "C:\\work\\file.ts", btoa("mine"), "etag-1", false, true);
    expect(active().conflict).toBeUndefined();
    expect(isDirty(active())).toBe(false);
  });
  it("reload replaces edits with current disk bytes and clears conflict", async () => {
    const id = await open();
    useFiles.getState().edit(id, "mine");
    mocks.write.mockResolvedValue({ conflict: true, current: info({ etag: "external" }) });
    await useFiles.getState().save(id);
    mocks.read.mockResolvedValue(read("external version", { info: info({ etag: "external" }) }));
    expect(await useFiles.getState().reload(id)).toBe(true);
    expect(active()).toMatchObject({ text: "external version", savedText: "external version", etag: "external" });
    expect(active().conflict).toBeUndefined();
    expect(isDirty(active())).toBe(false);
  });
  it("preserves edits and the deleted conflict when reloading fails", async () => {
    const id = await open();
    useFiles.getState().edit(id, "recover me");
    mocks.write.mockResolvedValue({ conflict: true, deleted: true });
    await useFiles.getState().save(id);
    mocks.read.mockRejectedValue(new Error("not found"));
    expect(await useFiles.getState().reload(id)).toBe(false);
    expect(active()).toMatchObject({ text: "recover me", conflict: { deleted: true }, error: "not found", loading: false });
  });
  it("never reports failed writes as saved", async () => {
    const id = await open(); useFiles.getState().edit(id, "mine");
    mocks.write.mockRejectedValue(new Error("disk full"));
    expect(await useFiles.getState().save(id)).toBe(false);
    expect(active()).toMatchObject({ savedText: "first\n", error: "disk full", saving: false });
    expect(isDirty(active())).toBe(true);
  });
});

describe("encoding and read-only safety", () => {
  it("round-trips UTF-8 BOM and CRLF without corrupting Unicode", () => {
    const encoded = encodeFile({ text: "const value = '你好';\n", bom: true, lineEnding: "\r\n" });
    const decoded = decodeFile(read("", { data: encoded }));
    expect(decoded).toMatchObject({ bom: true, lineEnding: "\r\n", text: "const value = '你好';\n", readOnly: false });
    expect(encodeFile(decoded)).toBe(encoded);
  });
  it.each([btoa("a\0b"), btoa(String.fromCharCode(0xff, 0xfe))])("refuses binary or invalid UTF-8 input", (data) => {
    expect(decodeFile(read("", { data }))).toMatchObject({ readOnly: true, text: "" });
  });
  it("does not edit or write read-only files", async () => {
    const id = await open(read("protected", { info: info({ readOnly: true }) }));
    useFiles.getState().edit(id, "changed");
    expect(active().text).toBe("protected");
    expect(await useFiles.getState().save(id, true)).toBe(false);
    expect(mocks.write).not.toHaveBeenCalled();
  });
  it("refuses truncated file saves and surfaces the read-size limit", async () => {
    expect(decodeFile(read("partial", { truncated: true }))).toMatchObject({ readOnly: true, text: "" });
    mocks.read.mockRejectedValue(new Error("too_large: file exceeds 2 MiB"));
    await useFiles.getState().openFile("local", "C:\\work\\large.txt");
    expect(active()).toMatchObject({ readOnly: true, loading: false, error: "too_large: file exceeds 2 MiB" });
  });
});

describe("file path navigation", () => {
  it("preserves absolute drive and filesystem roots", () => {
    expect(parentPath("C:\\file.ts")).toBe("C:\\");
    expect(parentPath("/file.ts")).toBe("/");
    expect(joinPath(parentPath("C:\\file.ts"), "renamed.ts")).toBe("C:\\renamed.ts");
    expect(joinPath(parentPath("/file.ts"), "renamed.ts")).toBe("/renamed.ts");
  });
  it("keeps remote namespace paths opaque and slash-separated", () => {
    expect(parentPath("/machine-id/home/user/file.ts")).toBe("/machine-id/home/user");
    expect(joinPath("/machine-id/home/user", "file.ts")).toBe("/machine-id/home/user/file.ts");
    expect(parentPath("/machine-id/C:/Users/name/file.ts")).toBe("/machine-id/C:/Users/name");
    expect(joinPath("/machine-id/C:/Users/name", "file.ts")).toBe("/machine-id/C:/Users/name/file.ts");
  });
});
