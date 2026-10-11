import { useEffect, useState } from "react";
import { ImageOff } from "lucide-react";
import { Agents } from "@/mygo";
import type { ImageStatusData } from "@/lib/taskChatTypes";
import { Dialog, DialogDescription, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { ImageGeneration } from "@/components/assistant-ui/elements/image-generation";
import { errText } from "@/features/agents/format";
import type { FeedItem, ImageInfo } from "@/lib/taskEvents";
import { useTaskCanvas } from "./TaskCanvasContext";
import { RASTER_MIMES } from "./TaskFiles";

const dataURLs = new Map<string, string>();
type ArtifactState = { state: "loading" } | { state: "ready"; src: string } | { state: "error"; message: string };

function useArtifactImage(sessionId: string, path: string) {
  const key = `${sessionId}\0${path}`;
  const [version, setVersion] = useState(0);
  const [result, setResult] = useState<{ key: string; version: number; value: ArtifactState } | null>(null);
  useEffect(() => {
    let active = true;
    if (!dataURLs.has(key)) void Agents.artifact(sessionId, path).then((artifact) => {
      if (!active) return;
      // Only the daemon's content type authorizes rendering, never a provider's MIME claim.
      if (RASTER_MIMES[artifact.mime] !== true) { setResult({ key, version, value: { state: "error", message: `Unsupported image type ${artifact.mime || "unknown"}.` } }); return; }
      const src = `data:${artifact.mime};base64,${artifact.data}`;
      if (dataURLs.size >= 24) dataURLs.delete(dataURLs.keys().next().value as string);
      dataURLs.set(key, src);
      setResult({ key, version, value: { state: "ready", src } });
    }, (e: unknown) => { if (active) setResult({ key, version, value: { state: "error", message: errText(e) } }); });
    return () => { active = false; };
  }, [key, sessionId, path, version]);
  const src = dataURLs.get(key);
  const value: ArtifactState = src ? { state: "ready", src } : result?.key === key && result.version === version ? result.value : { state: "loading" };
  return { value, retry: () => { dataURLs.delete(key); setVersion((v) => v + 1); } };
}

export function TaskImage({ sessionId, image, preview = false }: { sessionId: string; image: ImageInfo; preview?: boolean }) {
  const { value: art, retry } = useArtifactImage(sessionId, image.path);
  const canvas = useTaskCanvas();
  const [open, setOpen] = useState(false);
  const [failedSrc, setFailedSrc] = useState<string | null>(null);
  const alt = image.prompt || "Generated image";
  const decodeFailed = art.state === "ready" && failedSrc === art.src;
  return <figure className={preview ? "min-w-0 space-y-2 p-3" : "flex max-w-sm flex-col gap-2"} data-testid="image-card">
    {art.state === "loading" && <Skeleton role="status" aria-label="Loading image" className="aspect-square w-full max-w-72 rounded-md motion-reduce:animate-none" />}
    {(art.state === "error" || decodeFailed) && <div className="space-y-2 rounded-md border border-destructive/40 p-3"><p role="alert" className="flex gap-2 text-xs text-destructive-foreground"><ImageOff aria-hidden className="size-4 shrink-0" />Could not load the image: {art.state === "error" ? art.message : "The image could not be decoded."}</p><Button variant="outline" size="xs" onClick={() => { setFailedSrc(null); retry(); }}>Retry image</Button></div>}
    {art.state === "ready" && !decodeFailed && (preview
      ? <img src={art.src} alt={alt} className="mx-auto max-h-[65vh] max-w-full object-contain" onError={() => setFailedSrc(art.src)} />
      : <>
        <button type="button" aria-label={`Enlarge image: ${alt}`} onClick={() => canvas ? canvas.openImage(image) : setOpen(true)} className="cursor-zoom-in overflow-hidden rounded-md border outline-none focus-visible:ring-2 focus-visible:ring-ring"><img src={art.src} alt={alt} className="max-h-72 w-full object-contain" onError={() => setFailedSrc(art.src)} /></button>
        <Dialog open={open} onOpenChange={setOpen}><DialogPopup className="max-w-4xl"><DialogHeader><DialogTitle>Generated image</DialogTitle>{image.prompt && <DialogDescription className="selectable">{image.prompt}</DialogDescription>}</DialogHeader><DialogPanel><img src={art.src} alt={alt} className="mx-auto max-h-[70vh] max-w-full object-contain" /></DialogPanel></DialogPopup></Dialog>
      </>)}
    {image.prompt && <figcaption className="selectable break-words text-xs text-muted-foreground">{image.prompt}</figcaption>}
  </figure>;
}

export function ImageCard({ sessionId, item }: { sessionId: string; item: Extract<FeedItem, { type: "image" }> }) {
  return <TaskImage sessionId={sessionId} image={item.image} />;
}

/**
 * An image-generation tool call. While it runs the assistant-ui ImageGeneration frame holds the place (dot grid, shimmering
 * "Generating", the prompt); once the artifact exists the real `TaskImage` takes over, since the element never takes an image.
 * Only observed stages: no timers, synthetic pixels, resolution label, or regenerate button (there is no regenerate action).
 * The status/media/footer composition is adapted from BeUI image-generation, MIT © 2026 Saurabh Chauhan,
 * starc007/ui-components@cbc23255f962098a93a2c103d39e11f93b61d059, components/agents/image-generation.tsx.
 */
export function ImageGenerationCard({ generation, image, sessionId }: { generation: ImageStatusData; image?: ImageInfo; sessionId: string }) {
  const running = generation.status === "running";
  const failed = generation.status === "failed";
  return <div data-slot="image-generation-card" data-state={generation.status} aria-busy={running} className="w-full max-w-sm space-y-2">
    {image ? <TaskImage sessionId={sessionId} image={image} /> : running ? <ImageGeneration prompt={generation.prompt} generating /> : <>
      <div aria-hidden className="flex aspect-square max-w-72 items-center justify-center rounded-xl bg-muted"><ImageOff className="size-6 text-muted-foreground" /></div>
      <p role={failed ? "alert" : "status"} className="text-sm font-medium">{failed ? "Generation failed" : "Generation complete"}</p>
      {generation.prompt && <p className="selectable break-words text-xs text-muted-foreground">{generation.prompt}</p>}
      {!failed && <p className="text-xs text-muted-foreground">No image artifact was supplied.</p>}
    </>}
    {generation.error && <p role={failed ? undefined : "alert"} className="selectable whitespace-pre-wrap break-words text-xs text-destructive-foreground">{generation.error}</p>}
  </div>;
}
