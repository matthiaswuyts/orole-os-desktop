import { useEffect, useRef, useState } from "react";
import { load, mount } from "ascii.rest";
import { useSettings } from "@/stores/settings";
import { useUI } from "@/stores/ui";
import { decideMotion, useLowPower, useMedia } from "./AsciiBackground";

/** Text pieces (each draws into a <pre>, none needs a canvas) that may sit above the New task input. */
export const MARK_PIECES = ["donut", "gyroscope", "icosahedron", "black-hole", "planet", "rocket", "harmonograph", "campfire", "snake", "whale", "lighthouse", "coffee", "windmill"] as const;

export const pickMark = (random: () => number = Math.random): string => MARK_PIECES[Math.floor(random() * MARK_PIECES.length)]!;

// A monospace cell is about 0.6em wide; fit each piece into the same small box whatever its grid.
const WIDTH = 150;
const HEIGHT = 96;

/** One randomly chosen ascii.rest piece, small. Follows the ASCII settings: hidden when off, still when motion is reduced, paused or the window is unfocused. */
export function AsciiMark({ piece }: { /** Fixes the piece instead of picking one at random. */ piece?: string }) {
  const [chosen] = useState(() => piece ?? pickMark());
  const s = useSettings((x) => x.ascii);
  const loaded = useSettings((x) => x.loaded);
  const a11y = useSettings((x) => x.accessibility);
  const focused = useUI((x) => x.windowFocused);
  const sysReduced = useMedia("(prefers-reduced-motion: reduce)");
  const lowPower = useLowPower();
  const reduce = a11y.reduceMotion === "on" || (a11y.reduceMotion === "system" && s.respectReducedMotion && sysReduced);
  const m = decideMotion({ ...s, focused, reduceMotion: reduce, lowPower });
  const ref = useRef<HTMLPreElement>(null);
  const [fontSize, setFontSize] = useState(4);

  useEffect(() => {
    const el = ref.current;
    if (!el || !s.enabled || !loaded) return;
    const loader = (load as Record<string, () => Promise<Parameters<typeof mount>[1]>>)[chosen];
    if (!loader) return;
    let live = true;
    let stop: (() => void) | undefined;
    void loader().then((module) => {
      if (!live) return;
      if (typeof module !== "function") setFontSize(Math.min(WIDTH / (module.meta.cols * 0.6), HEIGHT / module.meta.rows));
      stop = mount(el, module, { fps: m.fps, motion: true });
    });
    return () => { live = false; stop?.(); };
  }, [chosen, s.enabled, loaded, m.fps]);

  if (!s.enabled || !loaded) return null;
  return <div aria-hidden="true" className="flex items-end" style={{ height: HEIGHT }}>
    <pre ref={ref} data-testid="ascii-mark" data-piece={chosen} data-motion={m.reason} className="m-0 select-none overflow-hidden whitespace-pre font-mono text-foreground/70" style={{ fontSize, lineHeight: 1 }} />
  </div>;
}
