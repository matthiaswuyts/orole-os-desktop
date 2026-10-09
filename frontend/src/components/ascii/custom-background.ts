import type { Piece } from "ascii.rest";

export interface CustomBackground {
  name: string;
  /** Base64 RGB samples, 200 × 100. No source file or external URL is retained. */
  pixels: string;
}

const COLS = 200;
const ROWS = 100;
const GROUND = "#080b12";
const BAYER = [0, 8, 2, 10, 12, 4, 14, 6, 3, 11, 1, 9, 15, 7, 13, 5];

export async function importBackground(file: File): Promise<CustomBackground> {
  if (!["image/png", "image/jpeg", "image/webp"].includes(file.type)) {
    throw new Error("Choose a PNG, JPEG, or WebP image.");
  }
  if (file.size > 10 * 1024 * 1024) throw new Error("Choose an image smaller than 10 MB.");
  const url = URL.createObjectURL(file);
  try {
    const image = new Image();
    image.src = url;
    await image.decode();
    const canvas = document.createElement("canvas");
    canvas.width = COLS;
    canvas.height = ROWS;
    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("Image conversion is unavailable in this window.");
    ctx.fillStyle = GROUND;
    ctx.fillRect(0, 0, COLS, ROWS);
    const scale = Math.max(COLS / image.naturalWidth, ROWS / image.naturalHeight);
    const width = image.naturalWidth * scale;
    const height = image.naturalHeight * scale;
    ctx.drawImage(image, (COLS - width) / 2, (ROWS - height) / 2, width, height);
    const rgba = ctx.getImageData(0, 0, COLS, ROWS).data;
    let rgb = "";
    for (let i = 0; i < rgba.length; i += 4) {
      rgb += String.fromCharCode(rgba[i]!, rgba[i + 1]!, rgba[i + 2]!);
    }
    return { name: file.name.slice(0, 160), pixels: btoa(rgb) };
  } catch (error) {
    if (error instanceof DOMException) throw new Error("This image could not be read. Try another PNG, JPEG, or WebP.");
    throw error;
  } finally {
    URL.revokeObjectURL(url);
  }
}

/** Match the built-in scenes' square-cell, coloured dot rendering via ascii.rest. */
export function customBackgroundPiece(image: CustomBackground): Piece {
  const rgb = atob(image.pixels);
  if (rgb.length !== COLS * ROWS * 3) throw new Error("Invalid custom background samples.");
  const palette = Array.from({ length: 64 }, (_, i) => {
    const levels = [0, 85, 170, 255];
    return `#${[levels[i >> 4]!, levels[(i >> 2) & 3]!, levels[i & 3]!].map((v) => v.toString(16).padStart(2, "0")).join("")}`;
  });
  const colors = new Uint8Array(COLS * ROWS);
  const lines: string[] = [];
  const quantize = (v: number, threshold: number) => Math.max(0, Math.min(3, Math.round(v / 85 + threshold)));
  for (let y = 0; y < ROWS; y++) {
    let line = "";
    for (let x = 0; x < COLS; x++) {
      const i = y * COLS + x;
      const r = rgb.charCodeAt(i * 3), g = rgb.charCodeAt(i * 3 + 1), b = rgb.charCodeAt(i * 3 + 2);
      const threshold = (BAYER[(y % 4) * 4 + x % 4]! + 0.5) / 16 - 0.5;
      colors[i] = quantize(r, threshold) * 16 + quantize(g, threshold) * 4 + quantize(b, threshold);
      const luminance = (r * 0.2126 + g * 0.7152 + b * 0.0722) / 255;
      line += luminance < 0.08 ? "·" : luminance + threshold * 0.15 < 0.3 ? "•" : "●";
    }
    lines.push(line);
  }
  const text = lines.join("\n");
  return {
    meta: { name: "custom background", category: "scenes", note: "uploaded image in coloured dots", cols: COLS, rows: ROWS, cell: 1, fps: 0, ground: GROUND, palette },
    default: () => (_t, env) => {
      env?.color?.set(colors);
      return text;
    },
  };
}
