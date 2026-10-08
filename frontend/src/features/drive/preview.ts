/** Rendering limits supplement the daemon's MIME, decoding and pixel limits. Never fetch a preview URL. */
export const MAX_PREVIEW_TEXT = 128 * 1024;
export const MAX_PREVIEW_DATA_URL = 1024 * 1024;

export interface PreviewContent {
  kind: string;
  mimeType: string;
  text: string;
  dataUrl: string;
  truncated: boolean;
}

export type SafePreview =
  | { kind: "text"; text: string; truncated: boolean }
  | { kind: "image"; src: string; truncated: boolean }
  | { kind: "unsupported" };

export function safePreview(content: PreviewContent): SafePreview {
  if (content.kind === "image") {
    if (content.dataUrl.length > MAX_PREVIEW_DATA_URL || !/^data:image\/(?:png|jpeg|gif|webp);base64,[A-Za-z0-9+/=\r\n]+$/.test(content.dataUrl)) return { kind: "unsupported" };
    return { kind: "image", src: content.dataUrl, truncated: content.truncated };
  }
  if (["text", "markdown", "json", "csv"].includes(content.kind)) {
    return { kind: "text", text: content.text.slice(0, MAX_PREVIEW_TEXT), truncated: content.truncated || content.text.length > MAX_PREVIEW_TEXT };
  }
  return { kind: "unsupported" };
}
