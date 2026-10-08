import { describe, expect, it } from "vitest";
import { parseInline, parseMarkdown, safeUrl } from "./mdparse";
import { parsePatch, pullFilesToDiff } from "./patch";
import type { PullFile } from "@/mygo";

describe("safeUrl", () => {
  it("allows http(s) and mailto only", () => {
    expect(safeUrl("https://github.com/a/b")).toBe("https://github.com/a/b");
    expect(safeUrl("mailto:a@b.c")).toBe("mailto:a@b.c");
    for (const bad of ["javascript:alert(1)", "JaVaScRiPt:alert(1)", "data:text/html,x", "vbscript:x", "file:///c:/x", "//evil.com", "/rel", " java\nscript:x", ""]) expect(safeUrl(bad)).toBeNull();
  });
});

describe("markdown", () => {
  it("never turns raw html into nodes", () => {
    const tree = parseMarkdown("<script>alert(1)</script> <img src=x onerror=alert(1)>");
    expect(JSON.stringify(tree)).toContain("<script>alert(1)</script>");
    expect(tree).toEqual([{ t: "p", c: [{ t: "text", v: "<script>alert(1)</script> <img src=x onerror=alert(1)>" }] }]);
  });
  it("drops unsafe link targets and keeps the text", () => {
    const inl = parseInline("[click](javascript:alert(1))");
    expect(inl.some((n) => n.t === "link")).toBe(false);
  });
  it("turns images into links without loading them", () => {
    const inl = parseInline("![shot](https://x.test/a.png)");
    expect(inl[0]).toMatchObject({ t: "link", href: "https://x.test/a.png" });
  });
  it("strips html comments, parses headings, code fences, lists, tasks", () => {
    const tree = parseMarkdown("<!-- hidden -->\n# Title\n\n```go\nfmt.Println(1)\n```\n\n- [x] done\n- [ ] todo\n\n1. one\n2. two\n\n> quote");
    expect(tree.map((b) => b.t)).toEqual(["heading", "code", "list", "list", "quote"]);
    const tasks = tree[2]!;
    expect(tasks.t === "list" && tasks.items.map((i) => i.checked)).toEqual([true, false]);
    expect(tree[1]).toMatchObject({ t: "code", lang: "go", v: "fmt.Println(1)" });
  });
  it("handles emphasis, inline code and autolinks", () => {
    const inl = parseInline("**bold** and `x*y*` see https://a.b/c.");
    expect(inl[0]).toMatchObject({ t: "strong" });
    expect(inl.find((n) => n.t === "code")).toMatchObject({ v: "x*y*" });
    expect(inl.find((n) => n.t === "link")).toMatchObject({ href: "https://a.b/c" });
  });
  it("terminates on pathological input", () => {
    const evil = "[".repeat(5000) + "*".repeat(5000) + "`".repeat(5000);
    expect(() => parseMarkdown(evil)).not.toThrow();
  });
});

describe("patch", () => {
  const patch = "@@ -1,3 +1,4 @@ func main\n a\n-b\n+c\n+d\n e\n\\ No newline at end of file";
  it("tracks line numbers", () => {
    const [h] = parsePatch(patch);
    expect(h!.lines.map((l) => [l.kind, l.old, l.new])).toEqual([[" ", 1, 1], ["-", 2, 0], ["+", 0, 2], ["+", 0, 3], [" ", 3, 4]]);
  });
  it("maps github statuses and flags patchless files binary", () => {
    const f = (o: Partial<PullFile>) => ({ filename: "x", previous_filename: "", status: "modified", additions: 1, deletions: 0, changes: 1, patch: "", ...o });
    const out = pullFilesToDiff([f({ status: "removed", patch }), f({ status: "added" })]);
    expect(out[0]!.status).toBe("deleted");
    expect(out[1]!.binary).toBe(true);
  });
});
