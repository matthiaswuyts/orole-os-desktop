import { useMemo } from "react";
import { FileText, Folder } from "lucide-react";
import { cn } from "@/lib/utils";

export interface TaskTreeFile { path: string; status?: string; additions?: number; deletions?: number }
interface TreeNode { name: string; folders: Map<string, TreeNode>; files: TaskTreeFile[] }

/** A display tree, not a filesystem browser; original paths still go through the daemon. */
export function buildTaskFileTree(files: TaskTreeFile[]): TreeNode {
  const root: TreeNode = { name: "", folders: new Map(), files: [] };
  const seen = new Set<string>();
  for (const file of files) {
    if (!file.path || seen.has(file.path)) continue;
    seen.add(file.path);
    const parts = file.path.replaceAll("\\", "/").split("/").filter(Boolean);
    let node = root;
    for (const name of parts.slice(0, Math.min(parts.length - 1, 32))) {
      let folder = node.folders.get(name);
      if (!folder) { folder = { name, folders: new Map(), files: [] }; node.folders.set(name, folder); }
      node = folder;
    }
    node.files.push(file);
  }
  return root;
}

export function TaskFileTree({ files, selected, onOpen }: { files: TaskTreeFile[]; selected?: string; onOpen: (path: string) => void }) {
  const root = useMemo(() => buildTaskFileTree(files), [files]);
  function branch(node: TreeNode) {
    return <ul className="space-y-0.5">{Array.from(node.folders.values()).sort((a, b) => a.name.localeCompare(b.name)).map((folder) => <li key={`folder:${folder.name}`}><details open><summary className="cursor-pointer rounded px-1 py-1 text-xs outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring"><Folder aria-hidden className="mr-1 inline size-3.5 text-muted-foreground" />{folder.name}</summary><div className="ml-3 border-l pl-2">{branch(folder)}</div></details></li>)}
      {node.files.map((file) => <li key={`file:${file.path}`}><button type="button" title={file.path} aria-current={selected === file.path ? "true" : undefined} onClick={() => onOpen(file.path)} className={cn("flex w-full min-w-0 items-center gap-2 rounded px-2 py-1.5 text-left text-xs outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring", selected === file.path && "bg-accent")}><FileText aria-hidden className="size-3.5 shrink-0 text-muted-foreground" /><span className="min-w-0 flex-1 break-all">{file.path.split(/[\\/]/).at(-1) || file.path}</span>{file.status && <span className="shrink-0 text-muted-foreground">{file.status}</span>}{file.additions !== undefined && <span className="shrink-0 font-mono tabular-nums text-success">+{file.additions}</span>}{file.deletions !== undefined && <span className="shrink-0 font-mono tabular-nums text-destructive-foreground">−{file.deletions}</span>}</button></li>)}
    </ul>;
  }
  return <nav aria-label="Task files" className="min-w-0 p-2">{files.length ? branch(root) : <p className="p-2 text-xs text-muted-foreground">No reported or changed files.</p>}</nav>;
}
