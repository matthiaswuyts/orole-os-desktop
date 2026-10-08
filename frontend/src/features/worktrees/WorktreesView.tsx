import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { useEffect, useMemo, useState } from "react";
import { GitBranch, Home, Plus, RefreshCw, Trash2 } from "lucide-react";
import type { Worktree } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { useProjects, worktreeState } from "@/stores/projects";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Spinner } from "@/components/ui/spinner";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Menu, MenuItem, MenuPopup, MenuTrigger } from "@/components/ui/menu";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { NeedsDesktop } from "@/features/shared/States";
import { ConfirmDialog, CreateWorktreeDialog } from "@/features/projects/dialogs";

export function WorktreesView() {
  const { projects, worktrees, loaded, loading, error, load, removeWorktree } = useProjects();
  const [creatingIn, setCreatingIn] = useState<string | null>(null);
  const [removing, setRemoving] = useState<Worktree | null>(null);
  const [force, setForce] = useState(false);

  useEffect(() => { void load(); }, [load]);

  const rows = useMemo(() => projects.flatMap((p) => (worktrees[p.id] ?? []).map((w) => ({ p, w }))), [projects, worktrees]);

  if (!inDesktop) return <NeedsDesktop what="Worktrees" />;
  if (!loaded) return <div role="status" className="flex flex-1 items-center justify-center gap-2 text-muted-foreground text-sm"><Spinner className="size-4" />Loading worktrees…</div>;
  if (error) return <div className="m-auto flex max-w-md flex-col items-center gap-2 text-center"><p role="alert" className="text-destructive-foreground text-sm">Could not load worktrees: {error}</p><Button variant="outline" onClick={() => void load()}>Retry</Button></div>;
  if (projects.length === 0) {
    return (
      <Empty>
        <EmptyHeader><EmptyMedia variant="icon"><GitBranch /></EmptyMedia><EmptyTitle>No worktrees yet</EmptyTitle>
          <EmptyDescription>Worktrees belong to projects. Add a project first.</EmptyDescription></EmptyHeader>
      </Empty>
    );
  }
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex h-9 shrink-0 items-center gap-2 border-b px-3">
        <span className="text-muted-foreground text-xs">{rows.length} {rows.length === 1 ? "worktree" : "worktrees"}</span>
        <span className="flex-1" />
        <Button size="xs" variant="outline" onClick={() => void load()} disabled={loading} aria-label="Refresh">{loading ? <Spinner className="size-3.5" /> : <RefreshCw />}</Button>
        <Menu>
          <MenuTrigger render={<Button size="xs" />}><Plus />New worktree</MenuTrigger>
          <MenuPopup align="end" className="min-w-44">
            {projects.filter((p) => !p.missing).map((p) => <MenuItem key={p.id} onClick={() => setCreatingIn(p.id)}>in {p.name}</MenuItem>)}
          </MenuPopup>
        </Menu>
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        <Table>
          <TableHeader><TableRow><TableHead>Project</TableHead><TableHead>Worktree</TableHead><TableHead>Branch</TableHead><TableHead>Ahead / behind</TableHead><TableHead>Dirty</TableHead><TableHead className="w-10"><span className="sr-only">Actions</span></TableHead></TableRow></TableHeader>
          <TableBody>
            {rows.map(({ p, w }) => {
              const state = worktreeState(w);
              return (
                <TableRow key={w.id}>
                  <TableCell className="font-medium">{p.name}</TableCell>
                  <TableCell>
                    <div className="flex items-center gap-1.5">{w.isMain ? <Home className="size-3.5 text-muted-foreground" aria-label="main checkout" /> : <GitBranch className="size-3.5 text-muted-foreground" />}<span>{w.isMain ? "main checkout" : w.name}</span>{w.locked && <Badge size="sm" variant="outline">locked</Badge>}{w.prunable && <Badge size="sm" variant="warning">missing</Badge>}</div>
                    <Tooltip><TooltipTrigger render={<div tabIndex={0} className="truncate font-mono text-[11px] text-muted-foreground"  />}>{w.path}</TooltipTrigger><TooltipPopup><span className="max-w-sm whitespace-pre-wrap break-words">{w.path}</span></TooltipPopup></Tooltip>
                  </TableCell>
                  <TableCell className="font-mono text-xs">{w.branch || (w.detached ? `detached @ ${w.head.slice(0, 7)}` : "-")}</TableCell>
                  <TableCell className="font-mono text-xs tabular-nums">{w.upstream ? `↑${w.ahead} ↓${w.behind}` : <span className="text-muted-foreground">no upstream</span>}</TableCell>
                  <TableCell>
                    {state === "error" ? <Tooltip><TooltipTrigger render={<span tabIndex={0}  className="text-destructive-foreground text-xs" />}>unreadable</TooltipTrigger><TooltipPopup><span className="max-w-sm whitespace-pre-wrap break-words">{w.error}</span></TooltipPopup></Tooltip>
                      : w.dirty === 0 ? <span className="text-muted-foreground text-xs">clean</span>
                      : <Tooltip><TooltipTrigger render={<Badge tabIndex={0} variant="warning"  />}>{w.dirty}</TooltipTrigger><TooltipPopup><span className="max-w-sm whitespace-pre-wrap break-words">{`${w.staged} staged, ${w.unstaged} unstaged, ${w.untracked} untracked`}</span></TooltipPopup></Tooltip>}
                  </TableCell>
                  <TableCell>{!w.isMain && <Button size="icon-xs" variant="ghost" aria-label={`Remove worktree ${w.name}`} onClick={() => { setForce(false); setRemoving(w); }}><Trash2 /></Button>}</TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </div>
      <CreateWorktreeDialog project={projects.find((p) => p.id === creatingIn)} open={creatingIn !== null} onOpenChange={(o) => !o && setCreatingIn(null)} />
      <ConfirmDialog open={removing !== null} onOpenChange={(o) => !o && setRemoving(null)} title={`Remove worktree ${removing?.name ?? ""}?`} confirmLabel="Remove worktree"
        onConfirm={async () => { if (removing) await removeWorktree(removing.id, force); }}
        extra={removing && removing.dirty > 0 ? (
          <label className="flex items-center gap-2 text-sm"><Checkbox checked={force} onCheckedChange={(c) => setForce(c === true)} />Discard {removing.dirty} uncommitted {removing.dirty === 1 ? "change" : "changes"}</label>
        ) : undefined}>
        <p>The checkout folder <code className="break-all font-mono">{removing?.path}</code> is deleted. The branch <code className="font-mono">{removing?.branch}</code> is kept.</p>
        {removing && removing.dirty > 0 && <p className="mt-1">It has uncommitted changes; git refuses to remove it unless you choose to discard them.</p>}
      </ConfirmDialog>
    </div>
  );
}
