import { useState } from "react";
import { ChevronRight, CircleX, Info, TriangleAlert, FileText, ExternalLink, ShieldAlert } from "lucide-react";
import type { Session } from "@/mygo";
import type { FileData, GuardrailData, SourceReference } from "@/lib/taskChatTypes";
import { Markdown } from "@/components/Markdown";
import { Badge } from "@/components/ui/badge";
import { Collapsible, CollapsiblePanel, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Dialog, DialogDescription, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { SyntaxHighlighter } from "@/components/SyntaxHighlighter";
import { duration, kindLabel } from "@/features/agents/format";
import { toolInputJSON, type FeedItem, type ToolCall, type ToolStatus } from "@/lib/taskEvents";
import { cn } from "@/lib/utils";
import { isWebURL, openExternal } from "@/lib/openExternal";
import { TaskApprovalCard } from "./TaskInteractions";
import { useTaskCanvas } from "./TaskCanvasContext";
import { TaskFilePreviewContent } from "./TaskFiles";
export { ImageCard, ImageGenerationCard } from "./TaskImages";

type Item<T extends FeedItem["type"]> = Extract<FeedItem, { type: T }>;

export function UserMessage({ item }: { item: Item<"user"> }) {
  return <div className="ml-auto w-fit max-w-[85%] rounded-lg bg-accent px-3 py-2 text-sm">
    <p className="sr-only">You said</p>
    <p className="selectable whitespace-pre-wrap break-words">{item.text}</p>
  </div>;
}

export function AssistantMessage({ item }: { item: Item<"assistant"> }) {
  return <div className="min-w-0 max-w-[92%]">
    <p className="sr-only">Agent said</p>
    <Markdown>{item.text}</Markdown>
  </div>;
}

const triggerClass = "group flex w-full min-w-0 items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring";
const chevron = <ChevronRight aria-hidden className="size-3.5 shrink-0 text-muted-foreground transition-transform group-data-[panel-open]:rotate-90 motion-reduce:transition-none" />;

const statusLabel: Record<ToolStatus, string> = { running: "Running", ok: "Completed", error: "Failed", unknown: "No result recorded" };

/**
 * What a tool step opens into: the raw tool name, its complete input and output. This is the technical record; the row above it
 * already says in plain words what the tool did.
 */
export function ToolDetails({ call }: { call: ToolCall }) {
  const input = toolInputJSON(call);
  return <div className="flex min-w-0 flex-col gap-2 text-xs" data-testid="tool-details" data-status={call.status}>
    <p className="text-muted-foreground">
      <span className="font-medium">Details</span> · <span className="font-mono" title={call.name}>{call.name}</span>{call.server ? ` · MCP server ${call.server}` : ""} · {statusLabel[call.status]}
    </p>
    {input && <section aria-label="Tool input"><p className="mb-1 font-medium text-muted-foreground">Input</p><SyntaxHighlighter code={input} language="json" /></section>}
    {call.output ? <section aria-label="Tool output"><p className="mb-1 font-medium text-muted-foreground">Output</p><SyntaxHighlighter code={call.output} /></section>
      : call.status !== "running" && <p className="text-muted-foreground">No output was recorded.</p>}
  </div>;
}

export function PermissionItem({ session, item }: { session: Session; item: Item<"permission"> }) {
  return <TaskApprovalCard sessionId={session.id} requester={kindLabel(session.kind)} request={item.request} resolved={!session.pending?.some((r) => r.id === item.request.id)} decision={item.decision} />;
}

export function NoteRow({ item }: { item: Item<"note"> }) {
  const Icon = item.kind === "needs_input" ? TriangleAlert : Info;
  return <p className={cn("flex items-start gap-1.5 text-xs", item.kind === "needs_input" ? "text-warning-foreground" : "text-muted-foreground")}><Icon aria-hidden className="mt-px size-3.5 shrink-0" /><span className="min-w-0 whitespace-pre-wrap break-words">{item.text}</span></p>;
}

export function ErrorRow({ item }: { item: Item<"error"> }) {
  return <div role="alert" data-slot="error-state" className="flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm"><CircleX aria-hidden className="mt-0.5 size-4 shrink-0 text-destructive-foreground" /><div className="min-w-0"><p className="font-medium text-destructive-foreground">Agent error</p><p className="selectable whitespace-pre-wrap break-words">{item.text || "The agent reported an error."}</p></div></div>;
}

/** A turn that ended badly: shown outside the collapsed run so the failure is never hidden. */
export function TurnFailure({ item }: { item: Item<"turn"> }) {
  return <div role="alert" data-slot="turn-failure" className="flex items-start gap-2 text-xs text-destructive-foreground">
    <CircleX aria-hidden className="mt-px size-3.5 shrink-0" />
    <div className="min-w-0"><p className="font-medium">Turn failed{item.durationMs === null ? "" : ` after ${duration(item.durationMs)}`}</p>{item.result && <p className="selectable whitespace-pre-wrap break-words">{item.result}</p>}</div>
  </div>;
}

/** The supplied http(s) sources as a list, each opened through the shell; null when none is safe to link. */
export function SourcesList({ sources }: { sources: SourceReference[] }) {
  const safe = sources.filter((source) => isWebURL(source.url));
  if (!safe.length) return null;
  return <ul className="divide-y rounded-md border" data-slot="sources-list">{safe.map((source, index) => <li key={`${source.id}:${index}`} className="min-w-0 p-3">
    <a href={source.url} target="_blank" rel="noopener noreferrer" className="break-words text-sm text-info-foreground underline underline-offset-2" onClick={(event) => { event.preventDefault(); void openExternal(source.url); }}>{source.title || new URL(source.url).hostname}</a>
    <p className="break-all text-xs text-muted-foreground">{source.url}</p>
    {source.snippet && <p className="selectable mt-1 whitespace-pre-wrap break-words text-xs">{source.snippet}</p>}
  </li>)}</ul>;
}

export function SourcesCard({ sources }: { sources: SourceReference[] }) {
  const count = sources.filter((source) => isWebURL(source.url)).length;
  if (!count) return null;
  return <Collapsible className="min-w-0 rounded-md border" data-slot="sources">
    <CollapsibleTrigger className={triggerClass}>{chevron}<ExternalLink aria-hidden className="size-3.5" /><span>Sources</span><span className="text-muted-foreground">{count}</span></CollapsibleTrigger>
    <CollapsiblePanel><div className="border-t"><SourcesList sources={sources} /></div></CollapsiblePanel>
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
