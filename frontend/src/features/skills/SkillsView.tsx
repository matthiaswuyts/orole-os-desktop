import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Download, Plus, RefreshCw, Search, Trash2 } from "lucide-react";
import { Skills, type Discovered, type Skill, type SyncState } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogDescription, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { NeedsDesktop } from "@/features/shared/States";
import { ConfirmDialog, errText, toastError } from "@/features/projects/dialogs";
import { draftProblem, filterSkills, gatewayBadge, groupDiscovered, harnessLabel, isSlug, mergeSyncStates, needsSync, retemplate, slugify, sourceLabel } from "./skills";

type Draft = { mode: "new" | "edit"; name: string; body: string; saved: string; files: string[] };

const SYNC_ALL = "*";

export function SkillsView() {
  const [list, setList] = useState<Skill[] | null>(null);
  const [listError, setListError] = useState<string | null>(null);
  const [states, setStates] = useState<SyncState[] | null>(null);
  const [statesError, setStatesError] = useState<string | null>(null);
  const [syncing, setSyncing] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [draft, setDraft] = useState<Draft | null>(null);
  const [opening, setOpening] = useState<string | null>(null);
  const openRequest = useRef(0);
  useEffect(() => () => { openRequest.current++; }, []);
  const [saving, setSaving] = useState(false);
  const [toggling, setToggling] = useState<ReadonlySet<string>>(new Set());
  const [deleting, setDeleting] = useState<string | null>(null);
  const [discarding, setDiscarding] = useState<(() => void) | null>(null);
  const [importing, setImporting] = useState(false);

  const loadList = useCallback(async () => {
    try { setList(await Skills.list()); setListError(null); } catch (e) { setListError(errText(e)); }
  }, []);
  const loadStates = useCallback(async () => {
    try { setStates(await Skills.syncState()); setStatesError(null); } catch (e) { setStatesError(errText(e)); }
  }, []);
  useEffect(() => {
    if (!inDesktop) return;
    void loadList();
    void loadStates();
  }, [loadList, loadStates]);

  const shown = useMemo(() => filterSkills(list ?? [], query), [list, query]);
  const dirty = draft !== null && (draft.mode === "new" ? draft.body !== "" : draft.body !== draft.saved);

  /** Runs `then` straight away, or after the user confirms discarding unsaved edits. */
  const guarded = (then: () => void) => { if (dirty) setDiscarding(() => then); else then(); };

  const edit = (name: string) => guarded(async () => {
    const request = ++openRequest.current;
    setOpening(name);
    try {
      const s = await Skills.get(name);
      if (request !== openRequest.current) return;
      setDraft({ mode: "edit", name: s.name, body: s.body, saved: s.body, files: s.files ?? [] });
    } catch (e) { if (request === openRequest.current) toastError("Could not open skill", e); }
    finally { if (request === openRequest.current) setOpening(null); }
  });

  const replaceDraft = (next: Draft | null) => {
    openRequest.current++;
    setOpening(null);
    setDraft(next);
  };
  const create = () => guarded(() => replaceDraft({ mode: "new", name: "", body: "", saved: "", files: [] }));

  const setName = (name: string) => setDraft((d) => (d ? { ...d, name, body: retemplate(d.body, d.name, name) } : d));

  async function save() {
    if (!draft) return;
    const name = draft.mode === "new" ? slugify(draft.name) : draft.name;
    setSaving(true);
    try {
      const s = await Skills.save(name, draft.body);
      setDraft({ mode: "edit", name: s.name, body: s.body, saved: s.body, files: s.files ?? [] });
      await Promise.all([loadList(), loadStates()]);
    } catch (e) { toastError("Could not save skill", e); }
    finally { setSaving(false); }
  }

  async function toggle(skill: Skill, enabled: boolean) {
    setToggling((t) => new Set(t).add(skill.name));
    try {
      const next = await Skills.setEnabled(skill.name, enabled);
      setList((l) => l?.map((s) => (s.name === next.name ? next : s)) ?? l);
      void loadStates();
    } catch (e) { toastError(`Could not ${enabled ? "enable" : "disable"} ${skill.name}`, e); void loadList(); }
    finally { setToggling((t) => { const n = new Set(t); n.delete(skill.name); return n; }); }
  }

  async function remove(name: string) {
    await Skills.delete(name);
    if (draft?.name === name) setDraft(null);
    await Promise.all([loadList(), loadStates()]);
  }

  async function sync(harness: string) {
    setSyncing(harness || SYNC_ALL);
    try {
      const next = await Skills.sync(harness);
      setStates((cur) => (cur ? mergeSyncStates(cur, next) : next));
      setStatesError(null);
    } catch (e) { toastError(harness ? `Could not sync ${harnessLabel(harness)}` : "Could not sync harnesses", e); void loadStates(); }
    finally { setSyncing(null); }
  }

  if (!inDesktop) return <NeedsDesktop what="Skills" />;

  const problem = draft ? draftProblem(draft.mode === "new" ? slugify(draft.name) : draft.name, draft.body) : null;
  const nameProblem = draft?.mode === "new" && draft.name !== "" && !isSlug(slugify(draft.name));

  return <div className="flex h-full min-h-0 flex-col" data-testid="skills-view">
    <header className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1 border-b px-4 py-2.5">
      <div className="min-w-0 flex-1">
        <h1 className="text-sm font-medium">Skills</h1>
        <p className="text-xs text-muted-foreground">Skills are served to every harness through the Orole gateway and synced into each harness&rsquo;s native skills folder.</p>
      </div>
      <Button size="sm" variant="ghost" aria-label="Refresh skills" onClick={() => { void loadList(); void loadStates(); }}><RefreshCw /></Button>
      <Button size="sm" variant="outline" onClick={() => setImporting(true)}><Download data-icon="inline-start" />Import from harnesses</Button>
      <Button size="sm" disabled={saving} onClick={create}><Plus data-icon="inline-start" />New skill</Button>
    </header>

    <HarnessStrip states={states} error={statesError} syncing={syncing} onSync={(h) => void sync(h)} onRetry={() => void loadStates()} />

    <div className="flex min-h-0 flex-1 flex-col sm:flex-row">
      <aside aria-label="Skills" className="flex max-h-72 shrink-0 flex-col border-b sm:max-h-none sm:w-80 sm:border-r sm:border-b-0">
        <div className="relative p-2">
          <Search aria-hidden className="pointer-events-none absolute top-1/2 left-4 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input aria-label="Search skills" className="[&_input]:ps-7" placeholder="Search skills" value={query} onChange={(e) => setQuery(e.target.value)} />
        </div>
        <div className="min-h-0 flex-1 overflow-auto px-2 pb-2">
          {listError && <p role="alert" className="p-2 text-destructive-foreground text-xs">{listError} <button className="underline" onClick={() => void loadList()}>Retry</button></p>}
          {!list && !listError && <div role="status" className="flex items-center gap-2 p-3 text-muted-foreground"><Spinner className="size-4" />Loading skills…</div>}
          {list && list.length === 0 && <Empty><EmptyHeader><EmptyTitle>No skills yet</EmptyTitle><EmptyDescription>Create a skill, or import the ones your harnesses already have.</EmptyDescription></EmptyHeader></Empty>}
          {list && list.length > 0 && shown.length === 0 && <p className="p-3 text-muted-foreground">No skill matches &ldquo;{query}&rdquo;.</p>}
          <ul className="flex flex-col gap-1">
            {shown.map((s) => {
              const selected = draft?.mode === "edit" && draft.name === s.name;
              return <li key={s.name} className={cn("flex items-start gap-2 rounded-md border border-transparent p-2 hover:bg-accent/50", selected && "border-border bg-accent")}>
                <button className="min-w-0 flex-1 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring" disabled={saving} aria-current={selected ? "true" : undefined} onClick={() => void edit(s.name)}>
                  <span className="flex items-center gap-1.5"><span className="truncate font-medium">{s.name}</span>{opening === s.name && <Spinner className="size-3" />}</span>
                  <span className="line-clamp-2 text-muted-foreground text-xs">{s.description || "No description"}</span>
                  <Badge size="sm" variant="outline" className="mt-1">{sourceLabel(s.source)}</Badge>
                </button>
                <Switch aria-label={`${s.enabled ? "Disable" : "Enable"} ${s.name}`} checked={s.enabled} disabled={toggling.has(s.name)} onCheckedChange={(v) => void toggle(s, v)} />
              </li>;
            })}
          </ul>
        </div>
      </aside>

      <section aria-label="Skill editor" className="flex min-h-0 min-w-0 flex-1 flex-col">
        {!draft ? <Empty className="flex-1"><EmptyHeader><EmptyTitle>Select or create a skill</EmptyTitle><EmptyDescription>A skill is a SKILL.md with a name and description in its frontmatter, followed by the instructions an agent follows.</EmptyDescription></EmptyHeader></Empty> : <>
          <div className="flex shrink-0 flex-wrap items-center gap-2 border-b px-4 py-2">
            {draft.mode === "new"
              ? <Input aria-label="Skill name" disabled={saving || opening !== null} aria-invalid={nameProblem || undefined} className="max-w-64 flex-1" placeholder="skill-name" value={draft.name} onChange={(e) => setName(e.target.value)} />
              : <h2 className="min-w-0 truncate font-medium">{draft.name}</h2>}
            {draft.mode === "edit" && dirty && <Badge variant="warning" size="sm">Unsaved</Badge>}
            <span className="ml-auto flex items-center gap-2">
              {draft.mode === "edit" && <Button size="sm" variant="ghost" disabled={saving || opening !== null} onClick={() => setDeleting(draft.name)}><Trash2 data-icon="inline-start" />Delete</Button>}
              <Button size="sm" variant="outline" disabled={saving} onClick={() => guarded(() => replaceDraft(null))}>Close</Button>
              <Button size="sm" disabled={saving || opening !== null || !dirty || problem !== null || (draft.mode === "new" && !isSlug(slugify(draft.name)))} onClick={() => void save()}>{saving ? "Saving…" : "Save"}</Button>
            </span>
          </div>
          <div className="min-h-0 flex-1 overflow-auto p-4">
            {draft.mode === "new" && <p className="mb-2 text-muted-foreground text-xs">The directory name is <code className="font-mono">{slugify(draft.name) || "…"}</code>. A name in the frontmatter takes precedence.</p>}
            <Textarea aria-label="SKILL.md" disabled={saving || opening !== null} className="font-mono" spellCheck={false} value={draft.body} onChange={(e) => setDraft((d) => (d ? { ...d, body: e.target.value } : d))} />
            {problem && draft.body !== "" && <p role="status" className="mt-2 text-warning-foreground text-xs">{problem}</p>}
            {draft.files.length > 0 && <div className="mt-3"><h3 className="mb-1 font-medium text-xs">Resource files</h3><ul className="font-mono text-muted-foreground text-xs">{draft.files.map((f) => <li key={f} className="selectable">{f}</li>)}</ul></div>}
          </div>
        </>}
      </section>
    </div>

    <ConfirmDialog open={deleting !== null} onOpenChange={(o) => { if (!o) setDeleting(null); }} title={`Delete ${deleting ?? "skill"}?`} confirmLabel="Delete skill" onConfirm={() => remove(deleting ?? "")}>
      Removes the skill from the Orole registry. Native copies in harness folders are removed on the next sync.
    </ConfirmDialog>
    <ConfirmDialog open={discarding !== null} onOpenChange={(o) => { if (!o) setDiscarding(null); }} title="Discard unsaved changes?" confirmLabel="Discard changes" onConfirm={async () => { discarding?.(); }}>
      The skill you are editing has changes that have not been saved.
    </ConfirmDialog>
    <ImportDialog open={importing} onOpenChange={setImporting} onImported={() => { void loadList(); void loadStates(); }} />
  </div>;
}

function HarnessStrip({ states, error, syncing, onSync, onRetry }: { states: SyncState[] | null; error: string | null; syncing: string | null; onSync: (harness: string) => void; onRetry: () => void }) {
  const installed = (states ?? []).filter((s) => s.available);
  return <section aria-label="Harnesses" className="shrink-0 border-b bg-sidebar/40 px-4 py-2">
    <div className="mb-1.5 flex items-center gap-2">
      <h2 className="font-medium text-xs">Harnesses</h2>
      <Button size="xs" variant="outline" className="ml-auto" disabled={syncing !== null || installed.length === 0} onClick={() => onSync("")}>{syncing === SYNC_ALL ? <Spinner data-icon="inline-start" /> : <RefreshCw data-icon="inline-start" />}Sync all</Button>
    </div>
    {error && <p role="alert" className="text-destructive-foreground text-xs">{error} <button className="underline" onClick={onRetry}>Retry</button></p>}
    {!states && !error && <div role="status" className="flex items-center gap-2 text-muted-foreground text-xs"><Spinner className="size-3.5" />Checking harnesses…</div>}
    <ul className="flex flex-wrap gap-2">
      {(states ?? []).map((s) => {
        const gw = gatewayBadge(s.gateway);
        return <li key={s.harness} className="flex min-w-60 flex-1 flex-col gap-1 rounded-lg border bg-card p-2">
          <div className="flex items-center gap-2">
            <span className="font-medium">{s.name || harnessLabel(s.harness)}</span>
            <Badge size="sm" variant={s.available ? "success" : "outline"}>{s.available ? "Installed" : "Not installed"}</Badge>
            <Button size="xs" variant="outline" className="ml-auto" disabled={!s.available || syncing !== null} aria-label={`Sync ${s.name || harnessLabel(s.harness)}`} onClick={() => onSync(s.harness)}>{syncing === s.harness ? <Spinner data-icon="inline-start" /> : <RefreshCw data-icon="inline-start" />}Sync</Button>
          </div>
          {s.available ? <>
            <p className="text-muted-foreground text-xs">
              {s.synced?.length ?? 0} synced · <span className={cn(s.outdated?.length && "text-warning-foreground")}>{s.outdated?.length ?? 0} outdated</span> · <span className={cn(s.missing?.length && "text-warning-foreground")}>{s.missing?.length ?? 0} missing</span>
            </p>
            <div className="flex flex-wrap items-center gap-1.5"><Badge size="sm" variant={gw.tone}>{gw.label}</Badge>{needsSync(s) && <span className="text-muted-foreground text-xs">Sync to update</span>}</div>
            {s.dir && <p className="selectable truncate font-mono text-muted-foreground text-xs" title={s.dir}>{s.dir}</p>}
          </> : <p className="text-muted-foreground text-xs">Install {s.name || harnessLabel(s.harness)} to sync skills into it.</p>}
          {s.error && <p role="alert" className="break-words text-destructive-foreground text-xs">{s.error}</p>}
        </li>;
      })}
    </ul>
  </section>;
}

function ImportDialog({ open, onOpenChange, onImported }: { open: boolean; onOpenChange: (o: boolean) => void; onImported: () => void }) {
  const [found, setFound] = useState<Discovered[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const key = (d: Discovered) => `${d.harness}/${d.name}`;

  const discover = useCallback(async () => {
    setFound(null); setError(null);
    try { setFound(await Skills.discover()); } catch (e) { setError(errText(e)); }
  }, []);
  useEffect(() => { if (open) void discover(); }, [open, discover]);

  async function run(items: Discovered[], id: string) {
    setBusy(id);
    const done = new Set<string>();
    try {
      for (const d of items) { await Skills.import(d.harness, d.name); done.add(key(d)); }
    } catch (e) { toastError("Could not import skill", e); }
    finally {
      setBusy(null);
      if (done.size) { setFound((f) => f?.filter((d) => !done.has(key(d))) ?? f); onImported(); }
    }
  }

  const groups = groupDiscovered(found ?? []);
  return <Dialog open={open} onOpenChange={(o) => { if (!busy) onOpenChange(o); }}>
    <DialogPopup className="max-w-2xl">
      <DialogHeader><DialogTitle>Import from harnesses</DialogTitle><DialogDescription>Skills found in your harnesses&rsquo; own folders that the Orole registry does not hold yet. Importing copies them into the registry.</DialogDescription></DialogHeader>
      <DialogPanel>
        {error && <p role="alert" className="text-destructive-foreground text-sm">{error} <button className="underline" onClick={() => void discover()}>Retry</button></p>}
        {!found && !error && <div role="status" className="flex items-center gap-2 text-muted-foreground"><Spinner className="size-4" />Scanning harness folders…</div>}
        {found && groups.length === 0 && <p className="text-muted-foreground">Nothing to import. Every skill in your harnesses is already in the registry.</p>}
        <div className="flex flex-col gap-4">
          {groups.map((g) => <section key={g.harness} aria-label={harnessLabel(g.harness)}>
            <div className="mb-1 flex items-center gap-2"><h3 className="font-medium text-sm">{harnessLabel(g.harness)}</h3><Badge size="sm" variant="outline">{g.skills.length}</Badge>
              <Button size="xs" variant="outline" className="ml-auto" disabled={busy !== null} onClick={() => void run(g.skills, g.harness)}>{busy === g.harness ? "Importing…" : "Import all"}</Button></div>
            <ul className="flex flex-col divide-y rounded-lg border">
              {g.skills.map((d) => <li key={key(d)} className="flex items-center gap-2 p-2">
                <div className="min-w-0 flex-1"><p className="truncate font-medium">{d.name}</p><p className="line-clamp-2 text-muted-foreground text-xs">{d.description || "No description"}</p><p className="selectable truncate font-mono text-muted-foreground text-xs" title={d.path}>{d.path}</p></div>
                <Button size="xs" variant="outline" disabled={busy !== null} aria-label={`Import ${d.name} from ${harnessLabel(d.harness)}`} onClick={() => void run([d], key(d))}>{busy === key(d) ? "Importing…" : "Import"}</Button>
              </li>)}
            </ul>
          </section>)}
        </div>
      </DialogPanel>
    </DialogPopup>
  </Dialog>;
}
