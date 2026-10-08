import { useEffect, useState } from "react";
import { Brain, Check, ChevronRight, Circle, CircleX, Info, LoaderCircle, Plug, TriangleAlert, ImageOff } from "lucide-react";
import { Agents, type Session } from "@/mygo";
import { Markdown } from "@/components/Markdown";
import { Badge } from "@/components/ui/badge";
import { Collapsible, CollapsiblePanel, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Dialog, DialogDescription, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { PermissionCard, ErrorNote } from "@/features/agents/shared";
import { duration, errText } from "@/features/agents/format";
import { clip, toolInputJSON, toolPreview, type FeedItem, type ToolCall, type ToolStatus } from "@/lib/taskEvents";
import { cn } from "@/lib/utils";

type Item<T extends FeedItem["type"]> = Extract<FeedItem, { type: T }>;

const stamp = (at: string) => { const t = Date.parse(at); return Number.isFinite(t) ? new Date(t).toLocaleString() : undefined; };

export function UserMessage({ item }: { item: Item<"user"> }) {
  return <div className="ml-auto max-w-[85%] rounded-lg bg-accent px-3 py-2 text-sm" title={stamp(item.at)}>
    <p className="mb-1 text-xs font-medium text-muted-foreground">You</p>
    <p className="selectable whitespace-pre-wrap break-words">{item.text}</p>
  </div>;
}

export function AssistantMessage({ item }: { item: Item<"assistant"> }) {
  return <div className="min-w-0 max-w-[92%]" title={stamp(item.at)}>
    <p className="mb-1 text-xs font-medium text-muted-foreground">Agent</p>
    <Markdown>{item.text}</Markdown>
  </div>;
}

const triggerClass = "group flex w-full min-w-0 items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring";
const chevron = <ChevronRight aria-hidden className="size-3.5 shrink-0 text-muted-foreground transition-transform group-data-[panel-open]:rotate-90 motion-reduce:transition-none" />;

/** Collapsed by default: a one-line preview that expands to the full reasoning text. */
export function ReasoningBlock({ item }: { item: Item<"reasoning"> }) {
  const preview = clip(item.text, 140);
  return <Collapsible className="rounded-md border border-dashed text-xs" data-testid="reasoning">
    <CollapsibleTrigger className={triggerClass} aria-label={`Reasoning: ${preview || "empty"}`}>
      {chevron}<Brain aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
      <span className="shrink-0 font-medium text-muted-foreground">Thought</span>
      <span className="min-w-0 flex-1 truncate italic text-muted-foreground/80">{preview}</span>
    </CollapsibleTrigger>
    <CollapsiblePanel><div className="border-t px-3 py-2 text-muted-foreground"><Markdown className="text-xs">{item.text}</Markdown></div></CollapsiblePanel>
  </Collapsible>;
}

const statusLabel: Record<ToolStatus, string> = { running: "Running", ok: "Done", error: "Failed", unknown: "No result recorded" };

function ToolStatusIcon({ status }: { status: ToolStatus }) {
  const Icon = status === "running" ? LoaderCircle : status === "ok" ? Check : status === "error" ? CircleX : Circle;
  return <Icon aria-hidden className={cn("size-3.5 shrink-0", status === "running" && "animate-spin text-info motion-reduce:animate-none", status === "ok" && "text-success", status === "error" && "text-destructive", status === "unknown" && "text-muted-foreground")} />;
}

/** One tool call with its result folded in; input and output expand on demand. */
export function ToolCard({ call }: { call: ToolCall }) {
  const input = toolInputJSON(call);
  const preview = toolPreview(call);
  return <Collapsible className={cn("rounded-md border bg-muted/30 text-xs", call.status === "error" && "border-destructive/40")} data-testid="tool-card" data-status={call.status}>
    <CollapsibleTrigger className={triggerClass} aria-label={`${call.name}${call.server ? ` (${call.server})` : ""}: ${statusLabel[call.status]}${preview ? `, ${preview}` : ""}`}>
      {chevron}<ToolStatusIcon status={call.status} />
      <span className="shrink-0 font-mono font-medium">{call.name}</span>
      {call.server && <Badge variant="outline" size="sm" className="font-mono"><Plug aria-hidden />{call.server}</Badge>}
      <span className="min-w-0 flex-1 truncate font-mono text-muted-foreground">{preview}</span>
    </CollapsibleTrigger>
    <CollapsiblePanel>
      <div className="flex flex-col gap-2 border-t p-2">
        <p className="text-muted-foreground">{statusLabel[call.status]}{call.server ? ` · MCP server ${call.server}` : ""}</p>
        {input && <section aria-label="Tool input"><p className="mb-1 font-medium text-muted-foreground">Input</p><pre className="selectable max-h-72 overflow-auto whitespace-pre-wrap break-all rounded border bg-background p-2 font-mono">{input}</pre></section>}
        {call.output ? <section aria-label="Tool output"><p className="mb-1 font-medium text-muted-foreground">Output</p><pre className="selectable max-h-72 overflow-auto whitespace-pre-wrap break-all rounded border bg-background p-2 font-mono">{call.output}</pre></section>
          : call.status !== "running" && <p className="text-muted-foreground">No output was recorded.</p>}
      </div>
    </CollapsiblePanel>
  </Collapsible>;
}

// Generated images only: the daemon returns bytes for files in the session's scratch directory and
// the card builds a data: URL itself, so no model-chosen URL is ever loaded.
const IMAGE_MIMES = ["image/png", "image/jpeg", "image/gif", "image/webp"];
const MAX_CACHED = 24;
const dataURLs = new Map<string, string>();

type ArtifactState = { state: "loading" } | { state: "ready"; src: string } | { state: "error"; message: string };

function useArtifactImage(sessionId: string, path: string, mime: string): ArtifactState {
  const key = `${sessionId}\u0000${path}`;
  const [result, setResult] = useState<{ key: string; value: ArtifactState } | null>(null);
  useEffect(() => {
    let active = true;
    if (!dataURLs.has(key)) Agents.artifact(sessionId, path).then((artifact) => {
      const type = IMAGE_MIMES.includes(artifact.mime) ? artifact.mime : IMAGE_MIMES.includes(mime) ? mime : "";
      if (!active) return;
      if (!type) { setResult({ key, value: { state: "error", message: `Unsupported image type ${artifact.mime || mime || "unknown"}.` } }); return; }
      const src = `data:${type};base64,${artifact.data}`;
      if (dataURLs.size >= MAX_CACHED) dataURLs.delete(dataURLs.keys().next().value as string);
      dataURLs.set(key, src);
      setResult({ key, value: { state: "ready", src } });
    }, (e: unknown) => { if (active) setResult({ key, value: { state: "error", message: errText(e) } }); });
    return () => { active = false; };
  }, [key, sessionId, path, mime]);
  const cached = dataURLs.get(key);
  if (cached) return { state: "ready", src: cached };
  return result?.key === key ? result.value : { state: "loading" };
}

export function ImageCard({ sessionId, item }: { sessionId: string; item: Item<"image"> }) {
  const { path, mime, prompt } = item.image;
  const art = useArtifactImage(sessionId, path, mime);
  const [open, setOpen] = useState(false);
  const alt = prompt || "Generated image";
  return <figure className="flex max-w-sm flex-col gap-1" data-testid="image-card">
    {art.state === "loading" && <Skeleton role="status" aria-label="Loading image" className="h-48 w-72 rounded-md" />}
    {art.state === "error" && <div role="alert" className="flex items-center gap-2 rounded-md border border-destructive/40 p-2 text-xs text-destructive-foreground"><ImageOff aria-hidden className="size-4 shrink-0" />Could not load the image: {art.message}</div>}
    {art.state === "ready" && <>
      <button type="button" aria-label={`Enlarge image: ${alt}`} onClick={() => setOpen(true)} className="cursor-zoom-in overflow-hidden rounded-md border outline-none focus-visible:ring-2 focus-visible:ring-ring"><img src={art.src} alt={alt} className="max-h-72 w-auto object-contain" /></button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogPopup className="max-w-4xl">
          <DialogHeader><DialogTitle>Generated image</DialogTitle>{prompt && <DialogDescription className="selectable">{prompt}</DialogDescription>}</DialogHeader>
          <DialogPanel><img src={art.src} alt={alt} className="mx-auto max-h-[70vh] w-auto object-contain" /></DialogPanel>
        </DialogPopup>
      </Dialog>
    </>}
    {prompt && <figcaption className="line-clamp-2 text-xs text-muted-foreground" title={prompt}>{prompt}</figcaption>}
  </figure>;
}

export function PermissionItem({ session, item }: { session: Session; item: Item<"permission"> }) {
  return <PermissionCard sessionId={session.id} request={item.request} resolved={!session.pending?.some((r) => r.id === item.request.id)} />;
}

export function NoteRow({ item }: { item: Item<"note"> }) {
  const Icon = item.kind === "needs_input" ? TriangleAlert : Info;
  return <p className={cn("flex items-start gap-1.5 text-xs", item.kind === "needs_input" ? "text-warning-foreground" : "text-muted-foreground")} title={stamp(item.at)}><Icon aria-hidden className="mt-px size-3.5 shrink-0" /><span className="min-w-0 whitespace-pre-wrap break-words">{item.text}</span></p>;
}

export function ErrorRow({ item }: { item: Item<"error"> }) {
  return <ErrorNote error={item.text || "The agent reported an error."} />;
}

export function TurnSeparator({ item }: { item: Item<"turn"> }) {
  const label = `${item.ok ? "Turn complete" : "Turn failed"}${item.durationMs === null ? "" : ` · ${duration(item.durationMs)}`}`;
  return <div role="separator" aria-label={label} className="flex flex-col gap-1" title={stamp(item.at)}>
    <div className="flex items-center gap-3 text-[11px] text-muted-foreground"><span className="h-px flex-1 bg-border" /><span className={cn(!item.ok && "text-destructive-foreground")}>{label}</span><span className="h-px flex-1 bg-border" /></div>
    {!item.ok && item.result && <p className="selectable whitespace-pre-wrap break-words text-center text-xs text-destructive-foreground">{item.result}</p>}
  </div>;
}
