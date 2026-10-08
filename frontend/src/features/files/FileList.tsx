import { useRef } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { File, Folder, Link, LockKeyhole, Pencil, Trash2 } from "lucide-react";
import type { Entry } from "@/mygo";
import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";

const sizeLabel = (bytes: number) => bytes < 1024 ? `${bytes} B` : bytes < 1024 ** 2 ? `${(bytes / 1024).toFixed(1)} KB` : `${(bytes / 1024 ** 2).toFixed(1)} MB`;
export function FileList({ entries, open, rename, remove, busy }: { entries: Entry[]; open: (entry: Entry) => void; rename: (entry: Entry) => void; remove: (entry: Entry) => void; busy: boolean }) {
  const scroll = useRef<HTMLDivElement>(null);
  const virtual = useVirtualizer({ count: entries.length, getScrollElement: () => scroll.current, estimateSize: () => 36, overscan: 8, getItemKey: (index) => entries[index]!.path });
  if (!entries.length) return <Empty><EmptyHeader><EmptyTitle>Empty folder</EmptyTitle><EmptyDescription>Create a file or folder to start working here.</EmptyDescription></EmptyHeader></Empty>;
  return <div role="table" aria-label="Files" aria-rowcount={entries.length + 1} className="flex min-h-0 flex-1 flex-col">
    <div role="row" className="grid h-8 shrink-0 grid-cols-[minmax(0,1fr)_70px_68px] items-center border-b px-3 text-[11px] text-muted-foreground"><span role="columnheader">Name</span><span role="columnheader" className="text-right">Size</span><span role="columnheader" className="sr-only">Actions</span></div>
    <div ref={scroll} className="min-h-0 flex-1 overflow-auto" data-testid="file-list">
      <div className="relative w-full" style={{ height: virtual.getTotalSize() }}>
        {virtual.getVirtualItems().map((row) => {
          const entry = entries[row.index]!;
          return <div role="row" aria-rowindex={row.index + 2} key={row.key} data-index={row.index} className="absolute left-0 top-0 grid w-full grid-cols-[minmax(0,1fr)_70px_68px] items-center border-b border-border/50 px-3 hover:bg-muted/40" style={{ height: row.size, transform: `translateY(${row.start}px)` }}>
            <div role="cell" className="min-w-0"><button type="button" onClick={() => open(entry)} className="flex h-8 w-full min-w-0 items-center gap-2 rounded-sm text-left text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring" title={`${entry.path}\nModified ${new Date(entry.modTime).toLocaleString()}`}>
              {entry.isDir ? <Folder className="size-3.5 shrink-0 text-muted-foreground" /> : <File className="size-3.5 shrink-0 text-muted-foreground" />}<span className="truncate">{entry.name}</span>{entry.link && <Link className="size-3 shrink-0 text-muted-foreground" aria-label="Link" />}{entry.readOnly && <LockKeyhole className="size-3 shrink-0 text-muted-foreground" aria-label="Read-only" />}{entry.git && <span className="ml-auto font-mono text-[10px] text-warning-foreground" title="Git status">{entry.git}</span>}
            </button></div>
            <span role="cell" className="text-right font-mono text-[10px] text-muted-foreground">{entry.isDir ? "—" : sizeLabel(entry.size)}</span>
            <div role="cell" className="flex justify-end gap-0.5"><Button variant="ghost" size="icon-xs" aria-label={`Rename ${entry.name}`} disabled={busy || entry.readOnly} onClick={() => rename(entry)}><Pencil /></Button><Button variant="ghost" size="icon-xs" aria-label={`Delete ${entry.name}`} disabled={busy || entry.readOnly} onClick={() => remove(entry)}><Trash2 /></Button></div>
          </div>;
        })}
      </div>
    </div>
  </div>;
}
