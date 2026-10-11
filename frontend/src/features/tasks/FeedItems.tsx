import { useState } from "react";
import { Brain, Check, ChevronRight, Circle, CircleX, Info, LoaderCircle, Plug, TriangleAlert, FileText, ExternalLink, ShieldAlert } from "lucide-react";
import type { FileData, GuardrailData, SourceReference } from "@/lib/taskChatTypes";
import { Markdown } from "@/components/Markdown";
import { Badge } from "@/components/ui/badge";
import { Collapsible, CollapsiblePanel, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Dialog, DialogDescription, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { SyntaxHighlighter } from "@/components/SyntaxHighlighter";
import { duration } from "@/features/agents/format";
import { clip, toolInputJSON, toolPreview, type FeedItem, type ToolCall, type ToolStatus } from "@/lib/taskEvents";
import { cn } from "@/lib/utils";
import { isWebURL, openExternal } from "@/lib/openExternal";
import { TaskApprovalCard } from "./TaskInteractions";
import { useTaskCanvas } from "./TaskCanvasContext";
import { TaskFilePreviewContent } from "./TaskFiles";
export { ImageCard, ImageGenerationCard } from "./TaskImages";

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
      <span className="min-w-0 max-w-[45%] truncate font-mono font-medium" title={call.name}>{call.name}</span>
      {call.server && <Badge variant="outline" size="sm" className="max-w-[30%] font-mono" title={call.server}><Plug aria-hidden /><span className="truncate">{call.server}</span></Badge>}
      <span className="min-w-0 flex-1 truncate font-mono text-muted-foreground">{preview}</span>
      <span className="shrink-0 text-muted-foreground">{statusLabel[call.status]}</span>
    </CollapsibleTrigger>
    <CollapsiblePanel>
      <div className="flex flex-col gap-2 border-t p-2">
        <p className="text-muted-foreground">{statusLabel[call.status]}{call.server ? ` · MCP server ${call.server}` : ""}</p>
        {input && <section aria-label="Tool input"><p className="mb-1 font-medium text-muted-foreground">Input</p><SyntaxHighlighter code={input} language="json" /></section>}
        {call.output ? <section aria-label="Tool output"><p className="mb-1 font-medium text-muted-foreground">Output</p><SyntaxHighlighter code={call.output} /></section>
          : call.status !== "running" && <p className="text-muted-foreground">No output was recorded.</p>}
      </div>
    </CollapsiblePanel>
  </Collapsible>;
}


export function PermissionItem({ sessionId, resolved, item }: { sessionId: string; resolved: boolean; item: Item<"permission"> }) {
  return <TaskApprovalCard sessionId={sessionId} request={item.request} resolved={resolved} decision={item.decision} />;
}

export function NoteRow({ item }: { item: Item<"note"> }) {
  const Icon = item.kind === "needs_input" ? TriangleAlert : Info;
  return <p className={cn("flex items-start gap-1.5 text-xs", item.kind === "needs_input" ? "text-warning-foreground" : "text-muted-foreground")} title={stamp(item.at)}><Icon aria-hidden className="mt-px size-3.5 shrink-0" /><span className="min-w-0 whitespace-pre-wrap break-words">{item.text}</span></p>;
}

export function ErrorRow({ item }: { item: Item<"error"> }) {
  return <div role="alert" data-slot="error-state" className="flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm"><CircleX aria-hidden className="mt-0.5 size-4 shrink-0 text-destructive-foreground" /><div className="min-w-0"><p className="font-medium text-destructive-foreground">Agent error</p><p className="selectable whitespace-pre-wrap break-words">{item.text || "The agent reported an error."}</p></div></div>;
}

export function TurnSeparator({ item }: { item: Item<"turn"> }) {
  const label = `${item.ok ? "Turn complete" : "Turn failed"}${item.durationMs === null ? "" : ` · ${duration(item.durationMs)}`}`;
  return <div role="separator" aria-label={label} className="flex flex-col gap-1" title={stamp(item.at)}>
    <div className="flex items-center gap-3 text-[11px] text-muted-foreground"><span className="h-px flex-1 bg-border" /><span className={cn(!item.ok && "text-destructive-foreground")}>{label}</span><span className="h-px flex-1 bg-border" /></div>
    {!item.ok && item.result && <p className="selectable whitespace-pre-wrap break-words text-center text-xs text-destructive-foreground">{item.result}</p>}
  </div>;
}

export function SourcesCard({ sources }: { sources: SourceReference[] }) {
  const safe = sources.filter((source) => isWebURL(source.url));
  if (!safe.length) return null;
  return <Collapsible className="min-w-0 rounded-md border" data-slot="sources">
    <CollapsibleTrigger className={triggerClass}>{chevron}<ExternalLink aria-hidden className="size-3.5" /><span>Sources</span><span className="text-muted-foreground">{safe.length}</span></CollapsibleTrigger>
    <CollapsiblePanel><ul className="divide-y border-t">{safe.map((source, index) => <li key={`${source.id}:${index}`} className="min-w-0 p-3">
      <a href={source.url} target="_blank" rel="noopener noreferrer" className="break-words text-sm text-info-foreground underline underline-offset-2" onClick={(event) => { event.preventDefault(); void openExternal(source.url); }}>{source.title || new URL(source.url).hostname}</a>
      <p className="break-all text-xs text-muted-foreground">{source.url}</p>
      {source.snippet && <p className="selectable mt-1 whitespace-pre-wrap break-words text-xs">{source.snippet}</p>}
    </li>)}</ul></CollapsiblePanel>
  </Collapsible>;
}

export function FileCard({ file, sessionId }: { file: FileData; sessionId: string }) {
  const canvas = useTaskCanvas();
  const [open, setOpen] = useState(false);
  const name = file.name || file.path.split(/[\\/]/).at(-1) || "Reported file";
  return <div className="min-w-0 rounded-md border p-3" data-slot="file">
    <div className="flex min-w-0 items-center gap-2"><FileText aria-hidden className="size-4 shrink-0 text-muted-foreground" /><div className="min-w-0 flex-1"><p className="truncate text-sm font-medium" title={name}>{name}</p><p className="break-all text-xs text-muted-foreground">{file.path}</p></div><Button size="xs" variant="outline" disabled={!file.path} onClick={() => canvas ? canvas.openFile(file.path) : setOpen(true)}>Preview</Button></div>
    {file.status && <p className="mt-1 text-xs text-muted-foreground">{file.status}</p>}
    {file.summary && <p className="selectable mt-2 whitespace-pre-wrap break-words text-xs">{file.summary}</p>}
    <Dialog open={open} onOpenChange={setOpen}><DialogPopup className="max-w-4xl"><DialogHeader><DialogTitle>{name}</DialogTitle><DialogDescription>Task file preview</DialogDescription></DialogHeader><DialogPanel>{open && <TaskFilePreviewContent key={`${sessionId}:${file.path}`} sessionId={sessionId} path={file.path} />}</DialogPanel></DialogPopup></Dialog>
  </div>;
}

export function GuardrailNotice({ notice }: { notice: GuardrailData }) {
  return <div role="status" data-slot="guardrail-notice" className="min-w-0 space-y-2 rounded-md border border-warning/40 bg-warning/5 p-3">
    <div className="flex flex-wrap items-center gap-2 text-sm font-medium"><ShieldAlert aria-hidden className="size-4 text-warning-foreground" /><span>Policy notice</span>{notice.kind && <Badge variant="outline" size="sm">{notice.kind}</Badge>}</div>
    <p className="selectable whitespace-pre-wrap break-words text-sm">{notice.message}</p>
    {notice.source && <p className="break-words text-xs text-muted-foreground">Reported by {notice.source}</p>}
  </div>;
}
