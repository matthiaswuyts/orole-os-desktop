import { useEffect, useState } from "react";
import { load, mount } from "ascii.rest";
import { SCENES } from "./scenes";

const PREVIEW_WIDTH = 480;
// Only catalog IDs enter this cache: at most one fixed-size PNG per scene.
const previews = new Map<string, Promise<string>>();
let previewQueue: Promise<unknown> = Promise.resolve();

/** Snapshot the real scene's first frame, with one temporary renderer at a time. */
function getScenePreview(scene: string): Promise<string> {
  const cached = previews.get(scene);
  if (cached) return cached;
  if (!SCENES.some((entry) => entry.id === scene)) return Promise.reject(new Error("Unknown scene"));

  const preview = previewQueue.then(async () => {
    const piece = await (load as Record<string, () => Promise<Parameters<typeof mount>[1]>>)[scene]!();
    const canvas = document.createElement("canvas");
    canvas.setAttribute("aria-hidden", "true");
    // mount uses CSS width × DPR; cap the actual bitmap, even on high-DPI screens.
    canvas.style.cssText = `position:fixed;left:-10000px;top:0;width:${PREVIEW_WIDTH / (window.devicePixelRatio || 1)}px;pointer-events:none;visibility:hidden`;
    document.body.append(canvas);
    let stop: (() => void) | undefined;
    try {
      stop = mount(canvas, piece, { fps: 0 });
      return canvas.toDataURL("image/png");
    } finally {
      stop?.();
      canvas.remove();
      canvas.width = canvas.height = 0;
    }
  });
  previews.set(scene, preview);
  // Failed loads may be retried on the next picker visit; don't poison the queue.
  previewQueue = preview.catch(() => { previews.delete(scene); });
  return preview;
}

export function ScenePreview({ scene }: { scene: string }) {
  const [preview, setPreview] = useState<{ scene: string; src?: string; failed?: boolean }>();
  useEffect(() => {
    let live = true;
    getScenePreview(scene).then(
      (src) => { if (live) setPreview({ scene, src }); },
      () => { if (live) setPreview({ scene, failed: true }); },
    );
    return () => { live = false; };
  }, [scene]);

  const current = preview?.scene === scene ? preview : undefined;
  return (
    <span aria-hidden="true" className="flex aspect-2/1 w-full items-center justify-center overflow-hidden bg-muted text-xs text-muted-foreground">
      {current?.src
        ? <img src={current.src} alt="" className="size-full object-cover" width={480} height={240} />
        : current?.failed ? "Preview unavailable" : "Loading preview…"}
    </span>
  );
}
