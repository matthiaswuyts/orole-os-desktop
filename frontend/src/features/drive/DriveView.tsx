import { Fragment, useEffect, useState } from "react";
import { File as FileIcon, Folder, Grid2X2, List } from "lucide-react";
import { Drive } from "@/mygo";
import type { DriveCapabilities } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Breadcrumb, BreadcrumbItem, BreadcrumbList, BreadcrumbPage, BreadcrumbSeparator } from "@/components/ui/breadcrumb";
import { Badge } from "@/components/ui/badge";
import { MCPSettings } from "@/features/mcp/MCPSettings";
import { errorText, Failure, Loading, NoResults, useRemote } from "@/features/mcp/shared";
import { VirtualList } from "@/features/mcp/VirtualList";
import { safePreview } from "./preview";
import { useUI } from "@/stores/ui";

export function DriveView() {
  const status = useRemote(() => Drive.status(), "drive-status");
  const [settings, setSettings] = useState(false);
  if (status.loading) return <Loading>Loading Drive connection…</Loading>;
  if (status.error) return <div className="p-4"><Failure error={status.error} retry={status.reload} /></div>;
  if (!inDesktop || !status.data?.connected || settings) return <div className="h-full overflow-auto p-4"><div className="mb-4 flex items-center gap-2"><h1 className="text-base font-medium">Drive setup</h1>{status.data?.connected && <Button size="sm" variant="outline" onClick={() => setSettings(false)}>Back to Drive</Button>}</div><Failure error={status.data?.error ?? null} /><MCPSettings adapter="drive" /></div>;
  return <DriveBrowser key={status.data.serverId} capabilities={status.data.capabilities} setup={() => setSettings(true)} />;
}

type Location = { id: string; name: string };
function DriveBrowser({ capabilities, setup }: { capabilities: DriveCapabilities; setup: () => void }) {
  const [path, setPath] = useState<Location[]>([]);
  const parentId = path[path.length - 1]?.id ?? "";
  const [query, setQuery] = useState("");
  const [search, setSearch] = useState("");
  const [mode, setMode] = useState("list");
  const [pages, setPages] = useState<string[]>([""]);
  const token = pages[pages.length - 1]!;
  const [selected, setSelected] = useState<Location | null>(null);
  const [navigationError, setNavigationError] = useState<string | null>(null);
  const [navigating, setNavigating] = useState(false);
  const pending = useUI((state) => state.pendingDriveFile);
  useEffect(() => {
    if (!pending) return;
    setSearch(""); setQuery(""); setPages([""]); setNavigationError(null);
    if (pending.isFolder) { setPath([{ id: pending.id, name: pending.name }]); setSelected(null); }
    else { setPath([]); setSelected({ id: pending.id, name: pending.name }); }
    useUI.getState().clearDriveFile();
  }, [pending]);
  const files = useRemote(() => search ? Drive.search({ parentId, query: search, pageToken: token, limit: 0 }) : Drive.list({ parentId, query: "", pageToken: token, limit: 0 }), JSON.stringify([parentId, search, token]), search ? capabilities.canSearch : capabilities.canList);
  const preview = useRemote(() => Drive.preview(selected!.id), selected?.id ?? "", !!selected && capabilities.canPreview);
  async function openFolder(folder: Location) {
    setNavigating(true); setNavigationError(null);
    try {
      const next: Location[] = [folder];
      if (search && capabilities.canStat) {
        const seen = new Set([folder.id]);
        let current = await Drive.stat(folder.id);
        while (current.parentId) {
          if (seen.has(current.parentId) || next.length >= 32) throw new Error("The server returned a cyclic or overly deep folder path.");
          seen.add(current.parentId);
          current = await Drive.stat(current.parentId);
          next.unshift({ id: current.id, name: current.name });
        }
      }
      setPath(search ? next : [...path, folder]); setSearch(""); setQuery(""); setPages([""]); setSelected(null);
    } catch (cause) { setNavigationError(errorText(cause)); }
    finally { setNavigating(false); }
  }
  const content = preview.data ? safePreview(preview.data) : null;
  return <div className="flex h-full min-h-0 flex-col">
    <header className="flex flex-wrap items-center justify-end gap-2 border-b px-3 py-2"><ToggleGroup aria-label="File layout" value={[mode]} onValueChange={(values) => { if (values[0]) setMode(values[0]); }}><ToggleGroupItem value="list" aria-label="List layout"><List /></ToggleGroupItem><ToggleGroupItem value="grid" aria-label="Grid layout"><Grid2X2 /></ToggleGroupItem></ToggleGroup><Button size="sm" variant="ghost" onClick={() => { files.reload(); preview.reload(); }}>Refresh</Button><Button size="sm" variant="outline" onClick={setup}>Connection</Button></header>
    <div className="flex flex-wrap items-center gap-3 border-b p-3"><Breadcrumb aria-label="Drive location"><BreadcrumbList><BreadcrumbItem>{!path.length ? <BreadcrumbPage>Drive</BreadcrumbPage> : <Button size="sm" variant="ghost" disabled={navigating} onClick={() => { setPath([]); setPages([""]); setSelected(null); setSearch(""); setQuery(""); }}>Drive</Button>}</BreadcrumbItem>{path.map((location, index) => <Fragment key={location.id}><BreadcrumbSeparator /><BreadcrumbItem>{index === path.length - 1 ? <BreadcrumbPage className="max-w-48 truncate">{location.name}</BreadcrumbPage> : <Button size="sm" variant="ghost" disabled={navigating} onClick={() => { setPath(path.slice(0, index + 1)); setPages([""]); setSelected(null); setSearch(""); setQuery(""); }}>{location.name}</Button>}</BreadcrumbItem></Fragment>)}</BreadcrumbList></Breadcrumb><form className="ml-auto flex gap-2" onSubmit={(event) => { event.preventDefault(); setSearch(query.trim()); setPages([""]); setSelected(null); }}><Input aria-label="Search Drive" placeholder="Search Drive" disabled={!capabilities.canSearch || navigating} value={query} onChange={(event) => setQuery(event.target.value)} /><Button size="sm" type="submit" variant="outline" disabled={!capabilities.canSearch || navigating}>Search</Button></form></div>
    <Failure error={navigationError} />
    <div className="grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-[minmax(18rem,1fr)_minmax(18rem,1fr)]">
      <section aria-label="Drive files" aria-busy={navigating} className="flex min-h-0 flex-col border-r max-lg:min-h-64">
        {search && <p className="border-b px-3 py-2 text-xs text-muted-foreground">Search results for “{search}”</p>}
        <Failure error={files.error} retry={files.reload} />{files.loading && <Loading>Loading files…</Loading>}{navigating && <Loading>Opening folder…</Loading>}
        {!capabilities.canList && !search && <NoResults title="File listing unavailable">The connected server did not expose a compatible file-list tool.</NoResults>}
        {files.data && !files.data.files?.length && <NoResults title="No files">{search ? "Try another search." : "This folder is empty."}</NoResults>}
        {files.data && !navigating && (mode === "list" ? <VirtualList items={files.data.files ?? []} selected={selected?.id ?? ""} label="Drive files" onSelect={(file) => { if (file.isFolder) void openFolder(file); else setSelected(file); }} render={(file) => <div className="flex items-center gap-3">{file.isFolder ? <Folder className="size-5 shrink-0" /> : <FileIcon className="size-5 shrink-0" />}<span className="min-w-0 flex-1"><span className="block truncate font-medium">{file.name}</span><span className="block truncate text-xs text-muted-foreground">{file.isFolder ? "Folder" : `${file.mimeType || "File"} · ${file.size.toLocaleString()} bytes`}</span></span><span className="shrink-0 text-xs text-muted-foreground">{file.modifiedTime}</span></div>} /> : <div className="grid min-h-0 flex-1 auto-rows-max grid-cols-2 gap-2 overflow-auto p-3 xl:grid-cols-3">{files.data.files?.map((file) => <button key={file.id} aria-pressed={selected?.id === file.id} onClick={() => { if (file.isFolder) void openFolder(file); else setSelected(file); }} className="flex min-w-0 flex-col items-start gap-2 rounded-md border p-3 text-left hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring aria-pressed:bg-accent">{file.isFolder ? <Folder className="size-6" /> : <FileIcon className="size-6" />}<span className="w-full truncate font-medium">{file.name}</span><span className="w-full truncate text-xs text-muted-foreground">{file.isFolder ? "Folder" : file.mimeType || "File"}</span></button>)}</div>)}
        <div className="flex justify-between gap-2 border-t p-2"><Button size="sm" variant="outline" disabled={pages.length <= 1 || files.loading || navigating} onClick={() => { setPages((current) => current.slice(0, -1)); setSelected(null); }}>Previous</Button><span className="self-center text-xs text-muted-foreground">Page {pages.length}</span><Button size="sm" variant="outline" disabled={!files.data?.nextPageToken || files.loading || navigating || pages.includes(files.data?.nextPageToken ?? "")} onClick={() => { if (files.data?.nextPageToken) { setPages((current) => [...current, files.data!.nextPageToken]); setSelected(null); } }}>Next</Button></div>
      </section>
      <section aria-label="Safe file preview" className="min-h-0 overflow-auto p-4">
        {!selected ? <NoResults title="Select a file to preview">Text, Markdown, JSON and CSV are displayed as text. Only bounded raster image data is rendered; remote URLs, HTML and SVG are not loaded.</NoResults> : (
          <div className="flex flex-col gap-3">
            <h2 className="break-words font-medium">{selected.name}</h2>
            <Badge variant="outline">Untrusted file content</Badge>
            {!capabilities.canPreview && <NoResults title="Preview unavailable">The server did not expose a compatible, bounded preview tool.</NoResults>}
            <Failure error={preview.error} retry={preview.reload} />
            {preview.loading && <Loading>Loading safe preview…</Loading>}
            {preview.data && content && <>
              <p className="text-xs text-muted-foreground">{preview.data.mimeType}</p>
              {content.kind === "image" ? <img src={content.src} alt={selected.name} className="max-h-[60vh] max-w-full self-start object-contain" />
                : content.kind === "text" ? <pre className="selectable max-w-full whitespace-pre-wrap break-words text-sm leading-relaxed">{content.text}</pre>
                : <NoResults title="This format cannot be previewed safely">No external content was loaded.</NoResults>}
              {content.kind !== "unsupported" && content.truncated && <p role="status" className="text-xs text-muted-foreground">Preview truncated to keep content bounded.</p>}
            </>}
          </div>
        )}
      </section>
    </div>
  </div>;
}
