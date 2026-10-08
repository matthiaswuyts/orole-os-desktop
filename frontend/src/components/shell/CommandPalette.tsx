import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { Command as Cmdk } from "cmdk";
import { Dialog, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { fuzzyScore, getCommands, getProviders, getRecent, pushRecent, registryVersion, subscribeRegistry, type Command } from "@/lib/commands";
import { useUI } from "@/stores/ui";

interface ProviderState { loading: boolean; error?: string; items: Command[] }
const GROUP = "[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:text-muted-foreground";

export function CommandPalette() {
  const { paletteOpen, setPalette } = useUI();
  const [q, setQ] = useState("");
  const [filter, setFilter] = useState<string | null>(null);
  const [remote, setRemote] = useState<Record<string, ProviderState>>({});
  const version = useSyncExternalStore(subscribeRegistry, registryVersion);
  const searchController = useRef<AbortController | null>(null);
  const [actionError, setActionError] = useState("");
  const [running, setRunning] = useState(false);
  const [cancelled, setCancelled] = useState(false);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") { e.preventDefault(); setPalette(!useUI.getState().paletteOpen); }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [setPalette]);

  const reset = (open: boolean) => { if (!open) { searchController.current?.abort(); setQ(""); setFilter(null); setRemote({}); setActionError(""); setCancelled(false); } setPalette(open); };

  useEffect(() => {
    setRemote({});
    setCancelled(false);
    if (!paletteOpen || q.trim().length < 2) return;
    const ctrl = new AbortController();
    searchController.current = ctrl;
    const t = setTimeout(() => {
      for (const p of getProviders()) {
        if (filter ? filter !== p.id : p.explicitOnly) continue;
        setRemote((r) => ({ ...r, [p.id]: { loading: true, items: [] } }));
        let error: string | undefined;
        Promise.resolve().then(() => p.search(q.trim(), ctrl.signal, (message) => { error = message; })).then(
          (items) => !ctrl.signal.aborted && setRemote((r) => ({ ...r, [p.id]: { loading: false, items, error } })),
          (e) => !ctrl.signal.aborted && setRemote((r) => ({ ...r, [p.id]: { loading: false, items: [], error: e instanceof Error ? e.message : String(e) } })),
        );
      }
    }, 180);
    return () => { clearTimeout(t); ctrl.abort(); };
  }, [q, filter, paletteOpen, version]);

  const local = useMemo(() => {
    const all = getCommands();
    if (!q) {
      const recent = getRecent().map((id) => all.find((c) => c.id === id)).filter((c): c is Command => !!c);
      return { recent, groups: groupBy(all) };
    }
    const scored = all.map((c) => ({ c, s: Math.max(fuzzyScore(q, c.title), fuzzyScore(q, c.category + " " + c.title) * 0.8, ...(c.keywords ?? []).map((k) => fuzzyScore(q, k) * 0.7)) })).filter((x) => x.s > 0).sort((a, b) => b.s - a.s);
    return { recent: [], groups: groupBy(scored.map((x) => x.c)) };
  }, [q, paletteOpen, version]);

  const run = async (c: Command) => {
    if (running) return;
    setRunning(true); setActionError("");
    try { await c.run(); pushRecent(c.id); reset(false); }
    catch (error) { setActionError(error instanceof Error ? error.message : String(error)); }
    finally { setRunning(false); }
  };
  const providers = getProviders();
  return (
    <Dialog open={paletteOpen} onOpenChange={reset}>
      <DialogPopup showCloseButton={false} className="max-w-xl overflow-hidden">
        <DialogTitle className="sr-only">Command palette</DialogTitle>
        <Cmdk shouldFilter={false} label="Command palette" loop>
          <Cmdk.Input value={q} onValueChange={(value) => { searchController.current?.abort(); setRemote({}); setQ(value); }} placeholder="Search commands, repositories, mail, files…" className="w-full border-b bg-transparent px-3 py-2.5 text-sm outline-none" />
          {providers.length > 0 && (
            <div className="flex flex-wrap gap-1 border-b px-2 py-1" role="group" aria-label="Filter by provider">
              <Chip on={filter === null} onClick={() => { if (filter !== null) { searchController.current?.abort(); setRemote({}); setFilter(null); } }}>All</Chip>
              {providers.map((p) => <Chip key={p.id} on={filter === p.id} onClick={() => { if (filter !== p.id) { searchController.current?.abort(); setRemote({}); setFilter(p.id); } }}>{p.label}</Chip>)}
            </div>
          )}
          {!filter && <p className="px-3 py-1 text-xs text-muted-foreground">Select Mail, Drive or Remote files to search those services. Policy approval may be required.</p>}
          {filter && q.trim().length < 2 && <p role="status" className="px-3 py-1 text-xs text-muted-foreground">Type at least two characters to search.</p>}
          {Object.values(remote).some((state) => state.loading) && <button type="button" className="px-3 py-1 text-xs" onClick={() => { searchController.current?.abort(); setRemote({}); setCancelled(true); }}>Cancel search</button>}
          {cancelled && <p role="status" className="px-3 py-1 text-xs text-muted-foreground">Search cancelled. Change the query to search again. Any pending policy request remains under daemon control.</p>}
          {running && <p role="status" className="px-3 py-1 text-xs text-muted-foreground">Opening…</p>}
          {actionError && <p role="alert" className="px-3 py-1 text-sm text-destructive-foreground">{actionError}</p>}
          <Cmdk.List className="max-h-80 overflow-auto p-1">
            <Cmdk.Empty className="p-4 text-center text-muted-foreground text-sm">No matching commands.</Cmdk.Empty>
            {!filter && local.recent.length > 0 && <Cmdk.Group heading="Recent" className={GROUP}>{local.recent.map((c) => <Item key={"r" + c.id} c={c} run={run} />)}</Cmdk.Group>}
            {!filter && Object.entries(local.groups).map(([cat, cs]) => (
              <Cmdk.Group key={cat} heading={cat} className={GROUP}>{cs.map((c) => <Item key={c.id} c={c} run={run} />)}</Cmdk.Group>
            ))}
            {providers.map((p) => {
              if (q.trim().length < 2 || (filter ? filter !== p.id : p.explicitOnly)) return null;
              const st = remote[p.id];
              if (!st) return null;
              return (
                <Cmdk.Group key={p.id} heading={p.label} className={GROUP}>
                  {st.loading && <div role="status" className="px-2 py-1 text-muted-foreground text-sm">Searching…</div>}
                  {st.error && <div role="alert" className="px-2 py-1 text-destructive-foreground text-sm">{p.label}: {st.error}</div>}
                  {st.items.map((c) => <Item key={c.id} c={c} run={run} />)}
                </Cmdk.Group>
              );
            })}
          </Cmdk.List>
        </Cmdk>
      </DialogPopup>
    </Dialog>
  );
}

const Chip = ({ on, onClick, children }: { on: boolean; onClick: () => void; children: React.ReactNode }) => (
  <button type="button" aria-pressed={on} onClick={onClick} className={"rounded-sm px-1.5 py-0.5 text-xs " + (on ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-accent")}>{children}</button>
);
const Item = ({ c, run }: { c: Command; run: (c: Command) => void }) => (
  <Cmdk.Item value={c.id} onSelect={() => run(c)} className="flex cursor-pointer items-center gap-2 rounded-sm px-2 py-1.5 text-sm aria-selected:bg-accent [&_svg]:size-3.5 [&_svg]:text-muted-foreground">
    {c.icon}<span className="min-w-0"><span className="block truncate">{c.title}</span><span className="block truncate text-xs text-muted-foreground">{c.category}</span></span>
  </Cmdk.Item>
);
function groupBy(cs: Command[]) {
  const g: Record<string, Command[]> = {};
  for (const c of cs) (g[c.category] ??= []).push(c);
  return g;
}
