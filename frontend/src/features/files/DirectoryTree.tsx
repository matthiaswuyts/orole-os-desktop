import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { useEffect, useState } from "react";
import type { KeyboardEvent } from "react";
import { ChevronDown, ChevronRight, Folder, FolderOpen, Unplug } from "lucide-react";
import { Files } from "@/mygo";
import type { Entry, Root } from "@/mygo";
import { fileError } from "@/stores/files";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";

function Branch({ provider, path, label, selected, navigate, revision, depth = 0, missing, problem }: {
  provider: string; path: string; label: string; selected: string; navigate: (path: string) => void; revision: number; depth?: number; missing?: boolean; problem?: string;
}) {
  const [expanded, setExpanded] = useState(false);
  const [children, setChildren] = useState<Entry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    if (!expanded || missing) return;
    let cancelled = false;
    setLoading(true); setError("");
    Files.list(provider, path).then((entries) => { if (!cancelled) setChildren((entries ?? []).filter((e) => e.isDir && !e.link)); }, (e) => { if (!cancelled) setError(fileError(e)); }).finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [provider, path, expanded, revision, missing]);
  const key = (e: KeyboardEvent<HTMLButtonElement>) => {
    if (e.key === "ArrowRight") { e.preventDefault(); setExpanded(true); }
    if (e.key === "ArrowLeft") { e.preventDefault(); if (expanded) setExpanded(false); else e.currentTarget.closest("[role=group]")?.parentElement?.querySelector<HTMLButtonElement>("[role=treeitem]")?.focus(); }
  };
  return <li role="none">
    <Tooltip><TooltipTrigger render={<button type="button" role="treeitem" aria-expanded={expanded} aria-selected={selected === path} aria-disabled={missing} onKeyDown={key} onClick={() => { if (!missing) { navigate(path); setExpanded((v) => !v); } }} className={cn("flex h-8 w-full items-center gap-1.5 rounded-sm pr-2 text-left text-xs outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring", selected === path && "bg-accent", missing && "text-muted-foreground")} style={{ paddingLeft: 6 + depth * 14 }} />}>{expanded ? <ChevronDown className="size-3 shrink-0" /> : <ChevronRight className="size-3 shrink-0" />}{expanded ? <FolderOpen className="size-3.5 shrink-0 text-muted-foreground" /> : <Folder className="size-3.5 shrink-0 text-muted-foreground" />}<span className="truncate">{label}</span>{missing && <Unplug className="size-3 shrink-0" />}</TooltipTrigger><TooltipPopup><span className="max-w-sm whitespace-pre-wrap break-words">{problem || path}</span></TooltipPopup></Tooltip>
    {expanded && <ul role="group">{loading && <li role="none" className="px-5 py-1 text-xs text-muted-foreground">Loading…</li>}{error && <li role="none"><p role="alert" className="px-3 py-1 text-xs text-destructive-foreground">{error}</p></li>}{children.map((child) => <Branch key={child.path} provider={provider} path={child.path} label={child.name} selected={selected} navigate={navigate} revision={revision} depth={depth + 1} />)}</ul>}
  </li>;
}

export function DirectoryTree({ roots, provider, selected, navigate, revision, removable, onRemove }: {
  roots: Root[]; provider: string; selected: string; navigate: (path: string) => void; revision: number; removable: boolean; onRemove: (root: Root) => void;
}) {
  return <div className="min-h-0 flex-1 overflow-auto p-1">
    <ul role="tree" aria-label="Folders" onKeyDown={(e) => {
      if (!["ArrowUp", "ArrowDown", "Home", "End"].includes(e.key)) return;
      const items = Array.from(e.currentTarget.querySelectorAll<HTMLButtonElement>("[role=treeitem]"));
      const index = items.indexOf(document.activeElement as HTMLButtonElement);
      const next = e.key === "Home" ? 0 : e.key === "End" ? items.length - 1 : Math.max(0, Math.min(items.length - 1, index + (e.key === "ArrowDown" ? 1 : -1)));
      e.preventDefault(); items[next]?.focus();
    }}>{roots.map((root) => <Branch key={root.id} provider={provider} path={root.path} label={root.label} selected={selected} navigate={navigate} revision={revision} missing={root.missing} problem={root.problem} />)}</ul>
    {removable && roots.length > 0 && <div className="mt-4 flex flex-col gap-1 border-t pt-2"><p className="px-2 text-[11px] text-muted-foreground">Approved folders</p>{roots.map((root) => <div key={root.id} className="flex min-w-0 items-center gap-1 px-2"><Tooltip><TooltipTrigger render={<span tabIndex={0} className="min-w-0 flex-1 truncate text-xs"  />}>{root.label}</TooltipTrigger><TooltipPopup><span className="max-w-sm whitespace-pre-wrap break-words">{root.path}</span></TooltipPopup></Tooltip><Button variant="ghost" size="xs" aria-label={`Remove root ${root.label}`} onClick={() => onRemove(root)}>Remove</Button></div>)}</div>}
  </div>;
}
