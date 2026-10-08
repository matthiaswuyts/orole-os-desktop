import { useEffect, useMemo, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { FileDiff, FileMinus2, FilePlus2, FilePen } from "lucide-react";
import { cn } from "@/lib/utils";
import { fileStartRows, flattenDiff, type DiffFile, type DiffStatus } from "@/lib/diff";

const ROW_H = 20;

const statusIcon: Record<DiffStatus, typeof FileDiff> = { added: FilePlus2, deleted: FileMinus2, modified: FilePen, renamed: FileDiff, copied: FileDiff };
const statusColor: Record<DiffStatus, string> = { added: "text-success", deleted: "text-destructive", modified: "text-muted-foreground", renamed: "text-info", copied: "text-info" };

export interface DiffViewProps {
  files: DiffFile[];
  /** True when the backend cut the diff at its size cap. */
  truncated?: boolean;
  className?: string;
}

/**
 * Unified diff viewer: file list on the left, one virtualized scroller with +/- coloring on the right.
 * Only visible rows are mounted, so very large diffs stay responsive.
 */
export function DiffView({ files, truncated, className }: DiffViewProps) {
  const rows = useMemo(() => flattenDiff(files), [files]);
  const starts = useMemo(() => fileStartRows(rows), [rows]);
  const scroller = useRef<HTMLDivElement>(null);
  const [active, setActive] = useState(0);
  const virt = useVirtualizer({ count: rows.length, getScrollElement: () => scroller.current, estimateSize: () => ROW_H, overscan: 20 });
  const items = virt.getVirtualItems();
  const first = items[0]?.index ?? 0;

  useEffect(() => {
    const row = rows[first];
    if (row) setActive(row.file);
  }, [first, rows]);

  const gutter = useMemo(() => {
    let max = 0;
    for (const f of files) for (const h of f.hunks) max = Math.max(max, h.oldStart + h.oldLines, h.newStart + h.newLines);
    return Math.max(3, String(max).length);
  }, [files]);

  if (files.length === 0) {
    return <div role="status" className={cn("flex items-center justify-center p-6 text-muted-foreground text-sm", className)}>No changes</div>;
  }
  const totalAdd = files.reduce((n, f) => n + f.additions, 0);
  const totalDel = files.reduce((n, f) => n + f.deletions, 0);

  return (
    <div className={cn("flex min-h-0 overflow-hidden rounded-lg border bg-card", className)} data-testid="diff-view">
      <nav aria-label="Changed files" className="flex w-56 shrink-0 flex-col border-r bg-sidebar">
        <div className="flex h-7 shrink-0 items-center justify-between border-b px-2 text-muted-foreground text-xs">
          <span>{files.length} {files.length === 1 ? "file" : "files"}</span>
          <span className="font-mono"><span className="text-success">+{totalAdd}</span> <span className="text-destructive">-{totalDel}</span></span>
        </div>
        <ul className="min-h-0 flex-1 overflow-auto py-1">
          {files.map((f, i) => {
            const Icon = statusIcon[f.status];
            return (
              <li key={`${f.path}:${i}`}>
                <button type="button" aria-current={active === i ? "true" : undefined} title={f.oldPath ? `${f.oldPath} → ${f.path}` : f.path}
                  onClick={() => { virt.scrollToIndex(starts[i] ?? 0, { align: "start" }); setActive(i); }}
                  className={cn("flex h-6 w-full items-center gap-1.5 px-2 text-left text-xs outline-none hover:bg-sidebar-accent focus-visible:ring-2 focus-visible:ring-ring", active === i && "bg-sidebar-accent")}>
                  <Icon className={cn("size-3 shrink-0", statusColor[f.status])} aria-label={f.status} />
                  <span className="min-w-0 flex-1 truncate">{f.path}</span>
                  {!f.binary && <span className="shrink-0 font-mono text-[10px] text-muted-foreground">{f.additions > 0 && <span className="text-success">+{f.additions}</span>}{f.additions > 0 && f.deletions > 0 && " "}{f.deletions > 0 && <span className="text-destructive">-{f.deletions}</span>}</span>}
                </button>
              </li>
            );
          })}
        </ul>
      </nav>
      <div className="flex min-w-0 flex-1 flex-col">
        {truncated && <div role="alert" className="shrink-0 border-b bg-warning/10 px-3 py-1 text-warning-foreground text-xs">Diff truncated: output exceeded the size limit. Narrow it to a path to see the rest.</div>}
        <div ref={scroller} role="region" aria-label="Diff" tabIndex={0} className="min-h-0 flex-1 overflow-auto font-mono text-xs leading-5 outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset">
          <div style={{ height: virt.getTotalSize(), minWidth: "max-content", position: "relative" }}>
            {items.map((v) => {
              const r = rows[v.index]!;
              return (
                <div key={v.key} data-index={v.index} style={{ position: "absolute", top: 0, left: 0, right: 0, height: ROW_H, transform: `translateY(${v.start}px)` }}>
                  {r.t === "file" && <FileHeader file={files[r.file]!} />}
                  {r.t === "note" && <div className="px-3 text-muted-foreground italic">{r.text}</div>}
                  {r.t === "hunk" && <div className="truncate bg-info/10 px-3 text-info">{r.header}</div>}
                  {r.t === "line" && (
                    <div className={cn("flex whitespace-pre", r.line.kind === "+" && "bg-success/12", r.line.kind === "-" && "bg-destructive/12")}>
                      <span aria-hidden className="select-none px-1 text-right text-muted-foreground/70" style={{ width: `${gutter + 1}ch`, boxSizing: "content-box" }}>{r.line.old || ""}</span>
                      <span aria-hidden className="select-none px-1 text-right text-muted-foreground/70" style={{ width: `${gutter + 1}ch`, boxSizing: "content-box" }}>{r.line.new || ""}</span>
                      <span aria-hidden className={cn("w-4 shrink-0 select-none text-center", r.line.kind === "+" && "text-success", r.line.kind === "-" && "text-destructive")}>{r.line.kind}</span>
                      <span className="pr-4">{r.line.text}</span>
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}

function FileHeader({ file }: { file: DiffFile }) {
  return (
    <div className="sticky top-0 flex items-center gap-2 border-y bg-muted px-3 font-sans text-foreground text-xs font-medium">
      <span className="truncate">{file.oldPath && file.oldPath !== file.path ? `${file.oldPath} → ${file.path}` : file.path}</span>
      <span className="font-mono font-normal"><span className="text-success">+{file.additions}</span> <span className="text-destructive">-{file.deletions}</span></span>
    </div>
  );
}
