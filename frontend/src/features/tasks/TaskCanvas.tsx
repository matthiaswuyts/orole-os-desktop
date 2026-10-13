import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { X } from "lucide-react";
import type { Session } from "@/mygo";
import { Button } from "@/components/ui/button";
import type { ImageInfo } from "@/lib/taskEvents";
import { cn } from "@/lib/utils";
import { TaskCanvasContext, type TaskCanvasActions } from "./TaskCanvasContext";
import { TaskFilePreviewContent } from "./TaskFiles";
import { TaskImage } from "./TaskImages";
export { useTaskCanvas } from "./TaskCanvasContext";

type Selection = { sessionId: string } & ({ kind: "file"; path: string } | { kind: "image"; image: ImageInfo });

export function TaskCanvas({ session, children }: { session: Session; children: ReactNode }) {
  const [selection, setSelection] = useState<Selection | null>(null);
  const current = selection?.sessionId === session.id ? selection : null;
  const opener = useRef<HTMLElement | null>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => { if (current) heading.current?.focus(); }, [current]);
  const open = useCallback((next: Selection) => {
    if (!selection || selection.sessionId !== session.id) opener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    setSelection(next);
  }, [selection, session.id]);
  const close = useCallback(() => { setSelection(null); opener.current?.focus(); }, []);
  const actions = useMemo<TaskCanvasActions>(() => ({
    openFile: (path) => open({ kind: "file", path, sessionId: session.id }),
    openImage: (image) => open({ kind: "image", image, sessionId: session.id }),
    close,
  }), [open, session.id, close]);
  const title = current?.kind === "file" ? current.path.split(/[\\/]/).at(-1) || current.path : "Generated image";
  return <TaskCanvasContext.Provider value={actions}>
    <div className="@container/canvas flex min-h-0 min-w-0 flex-1 flex-col" data-slot="canvas-split">
      <div className="flex min-h-0 min-w-0 flex-1 flex-col @4xl/canvas:flex-row">
        <div className={cn("flex min-h-0 min-w-0 flex-1 flex-col", current && "basis-1/2 @4xl/canvas:max-w-[48%]")}>{children}</div>
        {current && <section aria-label="Task canvas" className="flex min-h-0 min-w-0 flex-1 basis-1/2 flex-col border-t bg-background @4xl/canvas:border-t-0 @4xl/canvas:border-l">
          <header className="flex shrink-0 items-center gap-2 border-b px-3 py-2"><h2 ref={heading} tabIndex={-1} className="min-w-0 flex-1 truncate text-sm font-medium outline-none">{title}</h2><Button size="icon-xs" variant="ghost" aria-label="Close canvas" onClick={close}><X aria-hidden /></Button></header>
          <div className="min-h-0 flex-1 overflow-auto">
            {current.kind === "file" && <TaskFilePreviewContent key={`${session.id}:${current.path}`} sessionId={session.id} path={current.path} />}
            {current.kind === "image" && <TaskImage key={`${session.id}:${current.image.path}`} sessionId={session.id} image={current.image} preview />}
          </div>
        </section>}
      </div>
    </div>
  </TaskCanvasContext.Provider>;
}
