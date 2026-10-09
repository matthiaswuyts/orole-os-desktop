import { beforeEach, describe, expect, it, vi } from "vitest";

const bridge = vi.hoisted(() => ({ begin: vi.fn(), chunk: vi.fn(), commit: vi.fn(), discard: vi.fn() }));
vi.mock("@/mygo", () => ({
  Chat: { beginAttachment: bridge.begin, attachmentChunk: bridge.chunk, commitAttachment: bridge.commit, discardAttachment: bridge.discard },
}));

import { ATTACHMENT_PART, CHUNK_BYTES, MAX_ATTACHMENT_BYTES, MAX_ATTACHMENTS, createAttachmentAdapter, problemWith, uploadFile } from "./attachments";

const file = (size: number, name = "a.txt", type = "text/plain") => new File([new Uint8Array(size).fill(65)], name, { type });
const drain = async <T>(iterable: AsyncGenerator<T, void> | Promise<T>): Promise<T[]> => {
  if (iterable instanceof Promise) return [await iterable];
  const out: T[] = [];
  for await (const item of iterable) out.push(item);
  return out;
};

beforeEach(() => {
  Object.values(bridge).forEach((fn) => fn.mockReset());
  bridge.begin.mockResolvedValue({ id: "up1" });
  bridge.chunk.mockResolvedValue(undefined);
  bridge.commit.mockResolvedValue({ id: "att1", name: "a.txt", mime: "text/plain", size: 1 });
  bridge.discard.mockResolvedValue(undefined);
});

describe("problemWith (advisory validation)", () => {
  it("accepts images, PDFs and text-like files and leaves unknown types to the daemon", () => {
    for (const type of ["image/png", "image/jpeg", "image/gif", "image/webp", "application/pdf", "text/markdown", "application/json", "", "application/octet-stream"]) {
      expect(problemWith({ name: "f", size: 10, type }, 0, null)).toBeNull();
    }
  });
  it("rejects clearly unsupported types, empty and oversized files, and too many files", () => {
    expect(problemWith({ name: "v.mp4", size: 10, type: "video/mp4" }, 0, null)).toMatch(/not a supported file type/);
    expect(problemWith({ name: "e.txt", size: 0, type: "text/plain" }, 0, null)).toMatch(/empty/);
    expect(problemWith({ name: "big.txt", size: MAX_ATTACHMENT_BYTES + 1, type: "text/plain" }, 0, null)).toMatch(/larger than 20 MB/);
    expect(problemWith({ name: "x.txt", size: 1, type: "text/plain" }, MAX_ATTACHMENTS, null)).toMatch(/At most 10/);
  });
  it("explains when the model is known not to read images or PDFs", () => {
    expect(problemWith({ name: "a.png", size: 1, type: "image/png" }, 0, { vision: false, pdf: true })).toMatch(/cannot read images/);
    expect(problemWith({ name: "a.pdf", size: 1, type: "application/pdf" }, 0, { vision: true, pdf: false })).toMatch(/cannot read PDFs/);
    expect(problemWith({ name: "a.png", size: 1, type: "image/png" }, 0, { vision: true, pdf: false })).toBeNull();
  });
});

describe("uploadFile", () => {
  it("uploads in sequential 1 MiB base64 chunks and commits", async () => {
    expect(await uploadFile(file(CHUNK_BYTES * 2 + 5))).toBe("att1");
    expect(bridge.begin).toHaveBeenCalledWith({ name: "a.txt", size: CHUNK_BYTES * 2 + 5 });
    expect(bridge.chunk.mock.calls.map(([c]) => c.seq)).toEqual([0, 1, 2]);
    expect(bridge.chunk.mock.calls.map(([c]) => atob(c.dataBase64).length)).toEqual([CHUNK_BYTES, CHUNK_BYTES, 5]);
    expect(bridge.commit).toHaveBeenCalledWith("up1");
  });
  it("discards the staged upload when a chunk fails", async () => {
    bridge.chunk.mockRejectedValueOnce(new Error("bad_request: too big"));
    await expect(uploadFile(file(10))).rejects.toThrow("too big");
    expect(bridge.discard).toHaveBeenCalledWith("up1");
    expect(bridge.commit).not.toHaveBeenCalled();
  });
});

describe("attachment adapter", () => {
  const adapter = () => createAttachmentAdapter();

  it("rejects images up front once told the model cannot read them", async () => {
    const a = adapter();
    a.setSupport({ vision: false, pdf: true });
    const steps = await drain(a.add({ file: file(3, "p.png", "image/png") }));
    expect(steps[0]!.status).toMatchObject({ type: "incomplete", message: expect.stringContaining("cannot read images") });
    expect(bridge.begin).not.toHaveBeenCalled();
  });

  it("yields uploading, then staged; send carries the committed id", async () => {
    const a = adapter();
    const steps = await drain(a.add({ file: file(3) }));
    expect(steps.map((s) => s.status.type)).toEqual(["running", "requires-action"]);
    const sent = await a.send(steps.at(-1)!);
    expect(sent.status).toEqual({ type: "complete" });
    expect(sent.content).toEqual([{ type: "data", name: ATTACHMENT_PART, data: { id: "att1" } }]);
  });

  it("shows a failed upload as an error chip with the daemon's sentence", async () => {
    bridge.commit.mockRejectedValueOnce(new Error("bad_request: \"a.txt\" is not a supported file type"));
    const steps = await drain(adapter().add({ file: file(3) }));
    expect(steps.at(-1)!.status).toEqual({ type: "incomplete", reason: "error", message: "\"a.txt\" is not a supported file type" });
    expect(bridge.discard).toHaveBeenCalledWith("up1");
  });

  it("flags a rejected file without touching the daemon", async () => {
    const steps = await drain(adapter().add({ file: file(3, "v.mp4", "video/mp4") }));
    expect(steps).toHaveLength(1);
    expect(steps[0]!.status).toMatchObject({ type: "incomplete", reason: "error" });
    expect(bridge.begin).not.toHaveBeenCalled();
  });

  it("discards the staged file on remove", async () => {
    const a = adapter();
    const steps = await drain(a.add({ file: file(3) }));
    await a.remove(steps.at(-1)!);
    expect(bridge.discard).toHaveBeenCalledWith("att1");
  });

  it("refuses to send a file that has not finished uploading", async () => {
    const a = adapter();
    const gen = a.add({ file: file(3) }) as AsyncGenerator<Parameters<typeof a.send>[0], void>;
    const first = (await gen.next()).value!;
    await expect(a.send(first)).rejects.toThrow(/not finished uploading/);
  });
});
