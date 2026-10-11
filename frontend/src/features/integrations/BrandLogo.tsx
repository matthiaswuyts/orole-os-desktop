import { Plug } from "lucide-react";
import type { SimpleIcon } from "simple-icons";

/** Relative luminance of a 6-digit hex colour, 0 (black) to 1 (white). */
function luminance(hex: string): number {
  const [r, g, b] = [0, 2, 4].map((i) => {
    const channel = parseInt(hex.slice(i, i + 2), 16) / 255;
    return channel <= 0.03928 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4;
  }) as [number, number, number];
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

/**
 * A company's own mark from the bundled simple-icons set (CC0 paths, never server-supplied
 * markup). Near-black marks (GitHub, Vercel, Notion) follow the theme's foreground colour so
 * they stay visible on dark themes; every other mark keeps its brand colour. No icon: a plug.
 */
export function BrandLogo({ icon, className = "size-5" }: { icon?: SimpleIcon; className?: string }) {
  if (!icon) return <Plug aria-hidden className={className} />;
  const fill = luminance(icon.hex) < 0.08 ? "currentColor" : `#${icon.hex}`;
  return (
    <svg role="img" aria-label={icon.title} viewBox="0 0 24 24" className={className} fill={fill}>
      <path d={icon.path} />
    </svg>
  );
}
