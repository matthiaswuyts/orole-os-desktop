import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Files, GitCompareArrows, Image, RefreshCw, X } from "lucide-react";
import { Agents, type Event as AgentEvent, type ReviewDiff, type Session } from "@/mygo";
import { Button } from "@/components/ui/button";
import { DiffView } from "@/components/diff/DiffView";
import { errText, isRemoteProcess } from "@/features/agents/format";
import { groupEvents, type ImageInfo } from "@/lib/taskEvents";
import { toDiffFiles } from "@/lib/diff";
import { cn } from "@/lib/utils";
import { TaskCanvasContext, type TaskCanvasActions } from "./TaskCanvasContext";
import { TaskFileTree, type TaskTreeFile } from "./TaskFileTree";
import { TaskFilePreviewContent } from "./TaskFiles";
import { TaskImage } from "./TaskImages";
export { useTaskCanvas } from "./TaskCanvasContext";

type Selection = { sessionId: string } & ({ kind: "files" } | { kind: "changes" } | { kind: "file"; path: string } | { kind: "image"; image: ImageInfo });

export function TaskCanvas({ session, events, children }: { session: Session; events: AgentEvent[]; children: ReactNode }) {
  const [selection, setSelection] = useState<Selection | null>(null);
  const current = selection?.sessionId === session.id ? selection : null;
  const opener = useRef<HTMLElement | null>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  const [version, setVersion] = useState(0);
  const [result, setResult] = useState<{ key: string; diff?: ReviewDiff; error?: string } | null>(null);
  const remote = isRemoteProcess(session);
  const key = `${session.id}:${session.updatedAt}:${version}`;
  const enabled = !!current && !remote;
  useEffect(() => {
    if (!enabled) return;
    let active = true;
    void Agents.diff(session.id).then(
      (diff) => { if (active) setResult({ key, diff }); },
      (error: unknown) => { if (active) setResult({ key, error: errText(error) }); },
    );
    return () => { active = false; };
  }, [enabled, session.id, key]);
  useEffect(() => { if (current) heading.current?.focus(); }, [current]);
  const open = useCallback((next: Selection) => {
    if (!selection || selection.sessionId !== session.id) opener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    setSelection(next);
  }, [selection, session.id]);
  const close = useCallback(() => { setSelection(null); opener.current?.focus(); }, []);
  const actions = useMemo<TaskCanvasActions>(() => ({
    openFile: (path) => open({ kind: "file", path, sessionId: session.id }),
    openImage: (image) => open({ kind: "image", image, sessionId: session.id }),
    openChanges: () => open({ kind: "changes", sessionId: session.id }),
    close,
  }), [open, session.id, close]);
  const reported = useMemo(() => groupEvents(events, { live: session.live }), [events, session.live]);
  const diff = result?.key === key ? result.diff : undefined;
  const error = result?.key === key ? result.error || diff?.error : undefined;
  const changed = useMemo(() => diff && !diff.error ? toDiffFiles(diff.diff) : [], [diff]);
  const files = useMemo(() => {
    const merged = new Map<string, TaskTreeFile>();
    for (const item of reported) if (item.type === "file" && item.file.path) merged.set(item.file.path, { path: item.file.path, status: item.file.status });
    for (const file of changed) merged.set(file.path, { path: file.path, status: file.status, additions: file.binary ? undefined : file.additions, deletions: file.binary ? undefined : file.deletions });
    return Array.from(merged.values());
  }, [reported, changed]);
  const images = reported.flatMap((item) => item.type === "image" || item.type === "generation" && item.image ? [item.image!] : []);
  const title = current?.kind === "file" ? current.path.split(/[\\/]/).at(-1) || current.path : current?.kind === "image" ? "Generated image" : current?.kind === "changes" ? "Changes" : "Task files";
  return <TaskCanvasContext.Provider value={actions}>
    <div className="@container/canvas flex min-h-0 min-w-0 flex-1 flex-col" data-slot="canvas-split">
      <div className="flex shrink-0 flex-wrap items-center gap-1 border-b px-3 py-1">
        <Button variant="ghost" size="xs" onClick={() => open({ kind: "files", sessionId: session.id })}><Files aria-hidden />Task files{files.length ? ` (${files.length})` : ""}</Button>
        {!remote && <Button variant="ghost" size="xs" onClick={actions.openChanges}><GitCompareArrows aria-hidden />Open changes in canvas</Button>}
      </div>
      <div className="flex min-h-0 min-w-0 flex-1 flex-col @4xl/canvas:flex-row">
        <div className={cn("flex min-h-0 min-w-0 flex-1 flex-col", current && "basis-1/2 @4xl/canvas:max-w-[48%]")}>{children}</div>
        {current && <section aria-label="Task canvas" className="flex min-h-0 min-w-0 flex-1 basis-1/2 flex-col border-t bg-background @4xl/canvas:border-t-0 @4xl/canvas:border-l">
          <header className="flex shrink-0 items-center gap-2 border-b px-3 py-2"><h2 ref={heading} tabIndex={-1} className="min-w-0 flex-1 truncate text-sm font-medium outline-none" title={title}>{title}</h2>{current.kind !== "files" && <Button size="xs" variant="ghost" onClick={() => open({ kind: "files", sessionId: session.id })}>Files</Button>}<Button size="icon-xs" variant="ghost" aria-label="Close canvas" onClick={close}><X aria-hidden /></Button></header>
          <div className="min-h-0 flex-1 overflow-auto">
            {current.kind === "file" && <TaskFilePreviewContent key={`${session.id}:${current.path}`} sessionId={session.id} path={current.path} />}
            {current.kind === "image" && <TaskImage key={`${session.id}:${current.image.path}`} sessionId={session.id} image={current.image} preview />}
            {(current.kind === "files" || current.kind === "changes") && <>
              {remote ? <p className="p-3 text-sm text-muted-foreground">Local file previews and worktree changes are unavailable for SSH process sessions.</p> : error ? <div className="space-y-2 p-3"><p role="alert" className="selectable break-words text-sm text-destructive-foreground">Could not load changes: {error}</p><Button size="xs" variant="outline" onClick={() => setVersion((v) => v + 1)}><RefreshCw aria-hidden />Retry changes</Button></div> : !diff && <p role="status" className="p-3 text-sm text-muted-foreground">Loading changes…</p>}
              {current.kind === "changes" && diff && !error && <DiffView files={changed} truncated={diff.truncated || diff.diff.truncated} className="h-full min-h-64 rounded-none border-0" />}
              {current.kind === "files" && <><TaskFileTree files={files} onOpen={actions.openFile} />{diff && (diff.truncated || diff.diff.truncated) && <p className="px-3 text-xs text-warning-foreground">Changes were truncated; this file list may be incomplete.</p>}{images.length > 0 && <div className="border-t p-2"><p className="px-2 py-1 text-xs font-medium text-muted-foreground">Generated images</p>{images.map((image) => <button key={image.id || image.path} type="button" className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-xs outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring" onClick={() => actions.openImage(image)}><Image aria-hidden className="size-3.5 shrink-0" /><span className="min-w-0 truncate">{image.prompt || image.path.split(/[\\/]/).at(-1)}</span></button>)}</div>}</>}
            </>}
          </div>
        </section>}
      </div>
    </div>
  </TaskCanvasContext.Provider>;
}
