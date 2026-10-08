import { describe, expect, it } from "vitest";
import { fileStartRows, flattenDiff, toDiffFiles, type DiffFile } from "./diff";

const file = (path: string, over: Partial<DiffFile> = {}): DiffFile => ({ path, status: "modified", binary: false, additions: 0, deletions: 0, hunks: [], ...over });

describe("diff model", () => {
  it("flattens files, hunks and lines in order and records file starts", () => {
    const files: DiffFile[] = [
      file("a.ts", { additions: 1, deletions: 1, hunks: [{ header: "@@ -1,2 +1,2 @@", oldStart: 1, oldLines: 2, newStart: 1, newLines: 2, lines: [
        { kind: " ", text: "x", old: 1, new: 1 }, { kind: "-", text: "y", old: 2, new: 0 }, { kind: "+", text: "z", old: 0, new: 2 }] }] }),
      file("img.png", { binary: true }),
      file("b.ts", { status: "renamed", oldPath: "c.ts" }),
    ];
    const rows = flattenDiff(files);
    expect(rows.map((r) => r.t)).toEqual(["file", "hunk", "line", "line", "line", "file", "note", "file", "note"]);
    expect(fileStartRows(rows)).toEqual([0, 5, 7]);
    expect(rows[6]).toMatchObject({ text: "Binary file not shown" });
    expect(rows[8]).toMatchObject({ text: "Renamed from c.ts without changes" });
  });

  it("normalizes unknown backend values", () => {
    const [f] = toDiffFiles({ files: [{ path: "p", status: "weird", binary: false, additions: 0, deletions: 0, hunks: [{ header: "@@", oldStart: 1, oldLines: 1, newStart: 1, newLines: 1, lines: [{ kind: "?", text: "t", old: 1, new: 1 }] }] }] });
    expect(f!.status).toBe("modified");
    expect(f!.hunks[0]!.lines[0]!.kind).toBe(" ");
  });
});
