import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { useCallback, useEffect, useRef, useState } from "react";
import { AppWindow, FolderOpen, Pencil, Plus, RefreshCw, Rocket, Trash2 } from "lucide-react";
import { Applications, events, type Shortcut } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { NAV } from "@/lib/nav";
import { views } from "@/features/registry";
import { useUI } from "@/stores/ui";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Fieldset, FieldsetLegend } from "@/components/ui/fieldset";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Spinner } from "@/components/ui/spinner";

const errorText = (error: unknown) => error instanceof Error ? error.message : String(error);

export function ApplicationsView() {
  const [shortcuts, setShortcuts] = useState<Shortcut[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState("");
  const [query, setQuery] = useState("");
  const [editing, setEditing] = useState<Shortcut | "new" | null>(null);
  const [launching, setLaunching] = useState<string | null>(null);
  const [removing, setRemoving] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);
  const sequence = useRef(0);
  const refresh = useCallback(async () => {
    if (!inDesktop) return;
    const request = ++sequence.current;
    try {
      const items = await Applications.list();
      if (sequence.current === request) { setShortcuts(items); setError(null); setLoaded(true); }
    } catch (err) {
      if (sequence.current === request) { setError(errorText(err)); setLoaded(true); }
    }
  }, []);

  useEffect(() => {
    if (!inDesktop) return;
    const off = events.applicationsChanged.on(() => { void refresh(); });
    const offDaemon = events.daemonState.on(({ connected }) => { if (connected) void refresh(); });
    const onFocus = () => { void refresh(); };
    window.addEventListener("focus", onFocus);
    void refresh();
    return () => { sequence.current++; off(); offDaemon(); window.removeEventListener("focus", onFocus); };
  }, [refresh]);

  async function launch(shortcut: Shortcut) {
    setLaunching(shortcut.id); setError(null); setNotice("");
    try {
      const result = await Applications.launch(shortcut.id);
      setNotice(`Started ${shortcut.name} (process ${result.pid}).`);
    } catch (err) { setError(errorText(err)); }
    finally { setLaunching(null); }
  }

  async function remove(id: string) {
    setDeleting(true); setError(null);
    try {
      await Applications.remove(id);
      setRemoving(null);
      if (editing !== "new" && editing?.id === id) setEditing(null);
      await refresh();
    } catch (err) { setError(errorText(err)); }
    finally { setDeleting(false); }
  }

  const filter = query.trim().toLocaleLowerCase();
  const modules = NAV.filter((item) => views[item.id] && `${item.label} ${item.id}`.toLocaleLowerCase().includes(filter));
  const filtered = shortcuts.filter((item) => `${item.name} ${item.executable}`.toLocaleLowerCase().includes(filter));

  return (
    <div className="flex h-full min-h-0 flex-col overflow-auto">
      <header className="flex flex-wrap items-center gap-3 border-b px-4 py-3">
        <p className="mr-auto text-muted-foreground text-xs">Open an Orole module or a shortcut you configured.</p>
        <Button size="sm" variant="outline" disabled={!inDesktop} onClick={() => { setEditing("new"); setNotice(""); }}><Plus data-icon="inline-start" />Add shortcut</Button>
        <Button size="icon-sm" variant="ghost" aria-label="Refresh applications" disabled={!inDesktop} onClick={() => void refresh()}><RefreshCw /></Button>
      </header>
      <div className="flex flex-col gap-6 p-4">
        <Field><FieldLabel htmlFor="application-search">Find a module or application</FieldLabel><Input id="application-search" type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search applications" /></Field>
        {error && <Alert variant="error"><AlertTitle>Application action failed</AlertTitle><AlertDescription>{error}<Button size="sm" variant="outline" onClick={() => void refresh()}>Reload shortcuts</Button></AlertDescription></Alert>}
        {notice && <p role="status" className="text-muted-foreground text-sm">{notice}</p>}
        <section aria-labelledby="application-modules" className="flex flex-col gap-2">
          <h2 id="application-modules" className="font-medium text-sm">Orole modules</h2>
          <div className="flex flex-wrap gap-2">{modules.map((item) => <Button key={item.id} variant="outline" size="sm" onClick={() => useUI.getState().setView(item.id)}><item.icon data-icon="inline-start" />{item.label}</Button>)}</div>
          {modules.length === 0 && <p className="text-muted-foreground text-sm">No modules match your search.</p>}
        </section>
        {editing && <ShortcutEditor key={editing === "new" ? "new" : editing.id} shortcut={editing === "new" ? null : editing} onCancel={() => setEditing(null)} onSaved={() => { setEditing(null); void refresh(); }} />}
        <section aria-labelledby="application-shortcuts" className="flex flex-col gap-2">
          <h2 id="application-shortcuts" className="font-medium text-sm">Your shortcuts</h2>
          <p className="max-w-2xl text-muted-foreground text-xs">Native applications run with your Windows account permissions. Go-side policy approval is required before starting a process. Saving a shortcut does not authorize it.</p>
          {!inDesktop ? <Alert><AlertTitle>Desktop required</AlertTitle><AlertDescription>Configured shortcuts and native launches are available in the Windows desktop app. Module navigation works in this preview.</AlertDescription></Alert>
            : !loaded ? <p role="status" className="flex items-center gap-2 text-muted-foreground text-sm"><Spinner />Loading shortcuts…</p>
              : filtered.length === 0 ? <Empty><EmptyHeader><EmptyMedia variant="icon"><AppWindow /></EmptyMedia><EmptyTitle>{shortcuts.length ? "No matching shortcuts" : "Add your first application"}</EmptyTitle><EmptyDescription>{shortcuts.length ? "Try a different name or executable path." : "Choose a local executable. Orole stores its shortcut in the daemon so it remains available after restarting the desktop."}</EmptyDescription></EmptyHeader></Empty>
                : <ul className="divide-y rounded-lg border">{filtered.map((shortcut) => <li key={shortcut.id} className="flex flex-wrap items-center gap-2 px-3 py-3">
                  <AppWindow className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
                  <div className="min-w-0 flex-1"><h3 className="truncate font-medium text-sm">{shortcut.name}</h3><Tooltip><TooltipTrigger render={<p tabIndex={0} className="truncate font-mono text-muted-foreground text-xs"  />}>{shortcut.executable}</TooltipTrigger><TooltipPopup><span className="max-w-sm whitespace-pre-wrap break-words">{shortcut.executable}</span></TooltipPopup></Tooltip>{shortcut.arguments.length > 0 && <Tooltip><TooltipTrigger render={<p tabIndex={0} className="truncate font-mono text-muted-foreground text-xs"  />}>{JSON.stringify(shortcut.arguments)}</TooltipTrigger><TooltipPopup><span className="max-w-sm whitespace-pre-wrap break-words">{JSON.stringify(shortcut.arguments)}</span></TooltipPopup></Tooltip>}</div>
                  {removing === shortcut.id ? <div className="flex flex-wrap items-center gap-2"><span className="text-sm">Remove shortcut only?</span><Button size="sm" variant="destructive-outline" disabled={deleting} onClick={() => void remove(shortcut.id)}>Remove shortcut</Button><Button size="sm" variant="ghost" disabled={deleting} onClick={() => setRemoving(null)}>Cancel</Button></div> : <>
                    <Button size="sm" variant="outline" disabled={launching !== null} onClick={() => void launch(shortcut)}>{launching === shortcut.id ? <Spinner data-icon="inline-start" /> : <Rocket data-icon="inline-start" />}{launching === shortcut.id ? "Awaiting policy" : "Launch"}</Button>
                    <Button size="icon-sm" variant="ghost" aria-label={`Edit ${shortcut.name}`} onClick={() => setEditing(shortcut)}><Pencil /></Button>
                    <Button size="icon-sm" variant="ghost" aria-label={`Remove ${shortcut.name}`} onClick={() => setRemoving(shortcut.id)}><Trash2 /></Button>
                  </>}
                </li>)}</ul>}
        </section>
      </div>
    </div>
  );
}

function ShortcutEditor({ shortcut, onCancel, onSaved }: { shortcut: Shortcut | null; onCancel: () => void; onSaved: () => void }) {
  const [name, setName] = useState(shortcut?.name ?? "");
  const [executable, setExecutable] = useState(shortcut?.executable ?? "");
  const [argumentsJSON, setArgumentsJSON] = useState(JSON.stringify(shortcut?.arguments ?? []));
  const [workingDirectory, setWorkingDirectory] = useState(shortcut?.workingDirectory ?? "");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function pick() {
    setBusy(true); setError(null);
    try {
      const path = await Applications.pickExecutable();
      if (path) { setExecutable(path); if (!name) setName(path.split(/[\\/]/).pop()?.replace(/\.exe$/i, "") ?? ""); }
    } catch (err) { setError(errorText(err)); }
    finally { setBusy(false); }
  }
  async function save(event: React.FormEvent) {
    event.preventDefault(); setError(null);
    let args: unknown;
    try { args = JSON.parse(argumentsJSON); } catch { setError("Arguments must be a JSON array of strings, for example [\"--new-window\"]."); return; }
    if (!Array.isArray(args) || !args.every((item): item is string => typeof item === "string")) { setError("Arguments must be a JSON array containing only strings."); return; }
    setBusy(true);
    try {
      await Applications.save({ id: shortcut?.id ?? "", revision: shortcut?.revision ?? 0, name, executable, arguments: args, workingDirectory });
      onSaved();
    } catch (err) { setError(errorText(err)); }
    finally { setBusy(false); }
  }
  return (
    <form onSubmit={(event) => void save(event)}>
      <Fieldset disabled={busy} className="flex max-w-3xl flex-col gap-4 rounded-lg border p-4">
        <FieldsetLegend>{shortcut ? `Edit ${shortcut.name}` : "New application shortcut"}</FieldsetLegend>
        <Field><FieldLabel htmlFor="shortcut-name">Name</FieldLabel><Input id="shortcut-name" value={name} required maxLength={120} onChange={(event) => setName(event.target.value)} /></Field>
        <Field><FieldLabel htmlFor="shortcut-executable">Executable</FieldLabel><div className="flex w-full gap-2"><Input id="shortcut-executable" value={executable} required onChange={(event) => setExecutable(event.target.value)} /><Button type="button" variant="outline" disabled={busy} onClick={() => void pick()}><FolderOpen data-icon="inline-start" />Browse</Button></div><FieldDescription>Absolute path to a local .exe. Network paths, scripts and shell expansion are not supported.</FieldDescription></Field>
        <Field><FieldLabel htmlFor="shortcut-arguments">Arguments (JSON array)</FieldLabel><Input id="shortcut-arguments" value={argumentsJSON} onChange={(event) => setArgumentsJSON(event.target.value)} /><FieldDescription>One string per argument; use [] for none. Never enter passwords, API keys or tokens. Arguments are plain-text shortcut metadata and may appear in approvals.</FieldDescription></Field>
        <Field><FieldLabel htmlFor="shortcut-directory">Working directory (optional)</FieldLabel><Input id="shortcut-directory" value={workingDirectory} onChange={(event) => setWorkingDirectory(event.target.value)} /><FieldDescription>Leave empty to use the executable’s folder.</FieldDescription></Field>
        {error && <Alert variant="error"><AlertTitle>Could not save shortcut</AlertTitle><AlertDescription>{error}</AlertDescription></Alert>}
        <div className="flex gap-2"><Button type="submit" disabled={busy}>{busy && <Spinner data-icon="inline-start" />}Save shortcut</Button><Button type="button" variant="ghost" disabled={busy} onClick={onCancel}>Cancel</Button></div>
      </Fieldset>
    </form>
  );
}
