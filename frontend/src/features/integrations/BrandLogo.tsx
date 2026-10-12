import { Plug } from "lucide-react";

/** A company's official logo file, bundled with the app (never server-supplied). */
export interface Logo {
  src: string;
  /** Company name, for screen readers. */
  title: string;
  /** A single dark mark (GitHub, Vercel, ...): painted in the theme's foreground colour so it stays visible on dark themes. */
  mono?: boolean;
}

/** The company's own logo in its own colours. No logo: a plug. */
export function BrandLogo({ logo, className = "size-5" }: { logo?: Logo; className?: string }) {
  if (!logo) return <Plug aria-hidden className={className} />;
  if (logo.mono) {
    const mask = `url("${logo.src}") center / contain no-repeat`;
    return <span role="img" aria-label={logo.title} className={`${className} inline-block shrink-0 bg-foreground`} style={{ mask, WebkitMask: mask }} />;
  }
  return <img src={logo.src} alt={logo.title} draggable={false} className={`${className} shrink-0 object-contain`} />;
}
