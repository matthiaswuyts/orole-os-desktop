import { Fragment, useCallback, useEffect, useState } from "react";
import { FilePlus2, FolderPlus, RefreshCw } from "lucide-react";
import { Files, events } from "@/mygo";
import type { Entry, FileProvider, Root } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { containsPath, fileError, joinPath, useFiles } from "@/stores/files";
import { useUI } from "@/stores/ui";
import { Button } from "@/components/ui/button";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Breadcrumb, BreadcrumbItem, BreadcrumbLink, BreadcrumbList, BreadcrumbPage, BreadcrumbSeparator } from "@/components/ui/breadcrumb";
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { NeedsDesktop } from "@/features/shared/States";
import { ConfirmDialog } from "@/features/projects/dialogs";
import { EditorPane } from "@/features/editor/EditorPane";
import { DirectoryTree } from "./DirectoryTree";
import { FileList } from "./FileList";
import { DeleteFileDialog, FileNameDialog } from "./FileDialogs";
import type { FileMutation } from "./FileDialogs";

export function FilesView() {
  const provider = useFiles((s) => s.provider);
  const directory = useFiles((s) => s.directory);
  const [providers, setProviders] = useState<FileProvider[]>([]);
  const [roots, setRoots] = useState<Root[]>([]);
  const [rootProvider, setRootProvider] = useState("");
  const [entries, setEntries] = useState<Entry[]>([]);
  const [revision, setRevision] = useState(0);
  const [loading, setLoading] = useState(true);
  const [listing, setListing] = useState(false);
  const [error, setError] = useState("");
  const [listError, setListError] = useState("");
  const [adding, setAdding] = useState(false);
  const [mutation, setMutation] = useState<FileMutation | null>(null);
  const [deleting, setDeleting] = useState<Entry | null>(null);
  const [removingRoot, setRemovingRoot] = useState<Root | null>(null);
  const refresh = useCallback(() => setRevision((v) => v + 1), []);
  const navigate = useCallback((path: string) => useFiles.getState().setLocation(provider, path), [provider]);
  const source = providers.find((p) => p.id === provider);
  const root = roots.filter((r) => containsPath(r.path, directory)).sort((a, b) => b.path.length - a.path.length)[0];
  useEffect(() => {
    if (!inDesktop) return;
    let cancelled = false;
    setLoading(true); setError("");
    const load = async () => {
      const sources = await Files.providers();
      const source = sources.find((p) => p.id === provider);
      const loadedRoots = source?.connected ? (await Files.roots(provider)) ?? [] : [];
      if (cancelled) return;
      setProviders(sources); setRoots(loadedRoots); setRootProvider(provider);
      const current = useFiles.getState();
      if (current.provider === provider && !loadedRoots.some((r) => !r.missing && containsPath(r.path, current.directory))) {
        useFiles.getState().setLocation(provider, loadedRoots.find((r) => !r.missing)?.path ?? "");
      }
    };
    void load().catch((e) => { if (!cancelled) setError(fileError(e)); }).finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [provider, revision]);
  useEffect(() => {
    setEntries([]); setListError("");
    if (!inDesktop || !directory || !source?.connected) { setListing(false); return; }
    let cancelled = false;
    setListing(true);
    Files.list(provider, directory).then((items) => { if (!cancelled) setEntries((items ?? []).sort((a, b) => Number(b.isDir) - Number(a.isDir) || a.name.localeCompare(b.name, undefined, { numeric: true }))); }, (e) => { if (!cancelled) setListError(fileError(e)); }).finally(() => { if (!cancelled) setListing(false); });
    return () => { cancelled = true; };
  }, [provider, directory, source?.connected, revision]);
  useEffect(() => {
    if (!inDesktop) return;
    const off = [events.filesChanged.on(refresh), events.filesRootsChanged.on(refresh), events.projectsChanged.on(refresh)];
    window.addEventListener("focus", refresh);
    return () => { off.forEach((unsubscribe) => unsubscribe()); window.removeEventListener("focus", refresh); };
  }, [refresh]);
  const addRoot = async () => {
    setAdding(true); setError("");
    try { const added = await Files.addRoot(); if (added) { useFiles.getState().setLocation("local", added.path); refresh(); } }
    catch (e) { setError(fileError(e)); }
    finally { setAdding(false); }
  };
  const crumbs = root ? [{ label: root.label, path: root.path }] : [];
  if (root) for (const segment of directory.slice(root.path.length).split(/[\\/]/).filter(Boolean)) crumbs.push({ label: segment, path: joinPath(crumbs.at(-1)!.path, segment) });
  if (!inDesktop) return <NeedsDesktop what="Files" />;
  return <div className="flex h-full min-h-0 flex-col" data-testid="files-view">
    <header className="flex shrink-0 flex-wrap items-center gap-3 border-b px-4 py-2">
      <ToggleGroup aria-label="File providers" size="sm" value={[provider]} onValueChange={(values) => { if (values[0]) useFiles.getState().setLocation(String(values[0]), ""); }}>
        {providers.map((p) => <ToggleGroupItem key={p.id} value={p.id} aria-label={`${p.label} files`}>{p.label}</ToggleGroupItem>)}
      </ToggleGroup>
      <div className="ml-auto flex items-center gap-2">{source?.managedRoots && <Button size="sm" variant="outline" disabled={adding} onClick={() => void addRoot()}><FolderPlus data-icon="inline-start" />{adding ? "Choosing…" : "Add folder…"}</Button>}<Button variant="ghost" size="icon-sm" aria-label="Refresh files" onClick={refresh} disabled={loading}><RefreshCw /></Button></div>
    </header>
    {error && <p role="alert" className="border-b px-4 py-2 text-xs text-destructive-foreground">{error}</p>}
    <div className="flex min-h-0 flex-1 flex-col lg:flex-row">
      <section aria-label="File browser" className="flex min-h-64 min-w-0 flex-1 flex-col border-b lg:max-w-[50%] lg:border-r lg:border-b-0">
        {loading && (rootProvider !== provider || roots.length === 0) ? <p role="status" className="p-4 text-sm text-muted-foreground">Loading folders…</p>
          : !source || rootProvider !== provider ? <Empty><EmptyHeader><EmptyTitle>Files could not load</EmptyTitle><EmptyDescription>Check the daemon connection and retry.</EmptyDescription></EmptyHeader><EmptyContent><Button variant="outline" onClick={refresh}>Retry</Button></EmptyContent></Empty>
          : !source.connected ? <Empty><EmptyHeader><EmptyTitle>Not connected</EmptyTitle><EmptyDescription>{source.note || `${source.label} is not connected.`}</EmptyDescription></EmptyHeader></Empty>
          : roots.length === 0 ? <Empty>
            <EmptyHeader>
              <EmptyTitle>{provider === "git" ? "No projects registered" : provider === "remote" ? "No remote folders configured" : "Choose a folder"}</EmptyTitle>
              <EmptyDescription>{provider === "git" ? "Add a project to browse its files and git status here." : provider === "remote" ? "Configure a remote machine and its approved folders in Machines to browse its files here." : "Approve a local folder using the native folder picker. Orole only accesses folders you approve."}</EmptyDescription>
            </EmptyHeader>
            <EmptyContent>{source.managedRoots ? <Button disabled={adding} onClick={() => void addRoot()}><FolderPlus data-icon="inline-start" />Add folder…</Button>
              : provider === "remote" ? <Button onClick={() => useUI.getState().setView("machines")}>Open Machines</Button>
              : provider === "git" ? <Button onClick={() => useUI.getState().setView("projects")}>Open Projects</Button> : null}</EmptyContent>
          </Empty> : <>
          <div className="flex min-h-10 shrink-0 items-center gap-2 border-b px-3 py-1.5"><Breadcrumb className="min-w-0 flex-1"><BreadcrumbList>{crumbs.map((crumb, index) => <Fragment key={crumb.path}>{index > 0 && <BreadcrumbSeparator />}<BreadcrumbItem>{index === crumbs.length - 1 ? <BreadcrumbPage>{crumb.label}</BreadcrumbPage> : <BreadcrumbLink render={<button type="button" />} onClick={() => navigate(crumb.path)}>{crumb.label}</BreadcrumbLink>}</BreadcrumbItem></Fragment>)}</BreadcrumbList></Breadcrumb></div>
          <div className="flex min-h-0 flex-1">
            <aside aria-label="Folder tree" className="flex w-36 shrink-0 flex-col border-r xl:w-44"><DirectoryTree roots={roots} provider={provider} selected={directory} navigate={navigate} revision={revision} removable={source.managedRoots} onRemove={setRemovingRoot} /></aside>
            <div className="flex min-w-0 flex-1 flex-col"><div className="flex h-10 shrink-0 items-center gap-1 border-b px-2"><Button size="xs" variant="ghost" disabled={!directory || listing} onClick={() => setMutation({ kind: "file", directory })}><FilePlus2 data-icon="inline-start" />New file</Button><Button size="xs" variant="ghost" disabled={!directory || listing} onClick={() => setMutation({ kind: "folder", directory })}><FolderPlus data-icon="inline-start" />Folder</Button></div>
              {listing ? <p role="status" className="p-3 text-xs text-muted-foreground">Reading folder…</p> : listError ? <div className="flex flex-col gap-2 p-3"><p role="alert" className="text-xs text-destructive-foreground">{listError}</p><Button variant="outline" size="sm" onClick={refresh}>Retry</Button></div> : !directory ? <p className="p-3 text-xs text-muted-foreground">Choose an available root. Missing folders must be reconnected before browsing.</p> : <FileList entries={entries} busy={!!mutation || !!deleting} open={(entry) => { if (entry.isDir) navigate(entry.path); else void useFiles.getState().openFile(provider, entry.path); }} rename={(entry) => setMutation({ kind: "rename", entry })} remove={setDeleting} />}
              <p className="shrink-0 border-t px-3 py-1 text-[11px] text-muted-foreground">{entries.length.toLocaleString()} items</p>
            </div>
          </div>
        </>}
      </section>
      <EditorPane />
    </div>
    {mutation && <FileNameDialog action={mutation} provider={provider} close={() => setMutation(null)} changed={refresh} />}
    {deleting && <DeleteFileDialog entry={deleting} provider={provider} close={() => setDeleting(null)} changed={refresh} />}
    <ConfirmDialog open={!!removingRoot} onOpenChange={(open) => { if (!open) setRemovingRoot(null); }} title="Remove approved folder?" confirmLabel="Remove folder access" onConfirm={async () => { if (removingRoot) { await Files.removeRoot(removingRoot.id); refresh(); } }}>Orole will forget access to {removingRoot?.label}. Files remain on disk. Open buffers remain available, but cannot be saved unless their folder is approved again.</ConfirmDialog>
  </div>;
}
