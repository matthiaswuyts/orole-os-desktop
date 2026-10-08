import { describe, expect, it } from "vitest";
import { MAX_PREVIEW_DATA_URL, MAX_PREVIEW_TEXT, safePreview } from "./preview";
import type { PreviewContent } from "./preview";

const content: PreviewContent = { kind: "text", mimeType: "text/plain", text: "", dataUrl: "", truncated: false };

describe("safe Drive previews", () => {
  it("keeps HTML and Markdown as literal text rather than executable markup", () => {
    const text = '<img src="https://tracker.test/pixel"><script>alert(1)</script> [link](javascript:alert(1))';
    expect(safePreview({ ...content, kind: "markdown", text })).toEqual({ kind: "text", text, truncated: false });
  });

  it("bounds text and preserves server truncation", () => {
    expect(safePreview({ ...content, text: "x".repeat(MAX_PREVIEW_TEXT + 10) })).toEqual({ kind: "text", text: "x".repeat(MAX_PREVIEW_TEXT), truncated: true });
    expect(safePreview({ ...content, text: "partial", truncated: true })).toEqual({ kind: "text", text: "partial", truncated: true });
  });

  it("allows only inline raster image data, never server URLs or SVG", () => {
    for (const dataUrl of ["https://tracker.test/a.png", "file:///C:/secret.png", "javascript:alert(1)", "data:image/svg+xml;base64,PHN2Zz4=", "data:text/html;base64,PGltZz4=", "data:image/png;base64,broken!"]) {
      expect(safePreview({ ...content, kind: "image", dataUrl })).toEqual({ kind: "unsupported" });
    }
    expect(safePreview({ ...content, kind: "image", dataUrl: "data:image/png;base64,aGVsbG8=" })).toEqual({ kind: "image", src: "data:image/png;base64,aGVsbG8=", truncated: false });
  });

  it("rejects over-limit image data and unsupported formats", () => {
    expect(safePreview({ ...content, kind: "image", dataUrl: `data:image/png;base64,${"A".repeat(MAX_PREVIEW_DATA_URL)}` })).toEqual({ kind: "unsupported" });
    expect(safePreview({ ...content, kind: "html", text: "<h1>unsafe</h1>" })).toEqual({ kind: "unsupported" });
  });
});
