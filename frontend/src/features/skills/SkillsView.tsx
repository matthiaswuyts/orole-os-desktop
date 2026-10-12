import { useCallback, useEffect, useMemo, useState } from "react";
import { ChevronDown, Download, Plus, RefreshCw, ScrollText, Search } from "lucide-react";
import { Skills, type Skill, type SyncState } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Input } from "@/components/ui/input";
import { Menu, MenuItem, MenuPopup, MenuTrigger } from "@/components/ui/menu";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsList, TabsTab } from "@/components/ui/tabs";
import { NeedsDesktop } from "@/features/shared/States";
import { errText, toastError } from "@/features/projects/dialogs";
import { addedFromHub, filterSkills, harnessLabel, mergeSyncStates, timeAgo } from "./skills";
import { DiscoverTab } from "./DiscoverTab";
import { HarnessStatus } from "./HarnessStatus";
import { ImportDialog } from "./ImportDialog";
import { SkillPage } from "./SkillPage";
import { SourceBadge } from "./SourceBadge";

const SYNC_ALL = "*";
const NEW_WINDOW_S = 2 * 86400;

type Tab = "yours" | "discover";
/** `name` null is a skill being created. */
type Open = { name: string | null };

/**
 * Settings > Skills. "Yours" lists every skill with where it came from and when it was added; a
 * click opens the skill on its own page. "Discover" browses skills.sh. The agents strip says whether
 * Claude Code, Codex and Oh My Pi have the current skills.
 */
export function SkillsView() {
  const [list, setList] = useState<Skill[] | null>(null);
  const [listError, setListError] = useState<string | null>(null);
  const [states, setStates] = useState<SyncState[] | null>(null);
  const [statesError, setStatesError] = useState<string | null>(null);
  const [syncing, setSyncing] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [toggling, setToggling] = useState<ReadonlySet<string>>(new Set());
  const [importing, setImporting] = useState(false);
  const [tab, setTab] = useState<Tab>("yours");
  const [discoverSeen, setDiscoverSeen] = useState(false);
  const [discoverQuery, setDiscoverQuery] = useState("");
  const [page, setPage] = useState<Open | null>(null);

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

  const changed = () => { void loadList(); void loadStates(); };
  const shown = useMemo(
    () => [...filterSkills(list ?? [], query)].sort((a, b) => b.addedAt - a.addedAt || a.name.localeCompare(b.name)),
    [list, query],
  );

  async function toggle(skill: Skill, enabled: boolean) {
    setToggling((t) => new Set(t).add(skill.name));
    try {
      const next = await Skills.setEnabled(skill.name, enabled);
      setList((l) => l?.map((s) => (s.name === next.name ? next : s)) ?? l);
      void loadStates();
    } catch (e) { toastError(`Could not ${enabled ? "enable" : "disable"} ${skill.name}`, e); void loadList(); }
    finally { setToggling((t) => { const n = new Set(t); n.delete(skill.name); return n; }); }
  }

  /** Copies the enabled skills into the agents' own folders. Throws; callers decide how to show it. */
  async function syncAgents(harness: string) {
    setSyncing(harness || SYNC_ALL);
    try {
      const next = await Skills.sync(harness);
      setStates((cur) => (cur ? mergeSyncStates(cur, next) : next));
      setStatesError(null);
    } catch (e) { void loadStates(); throw e; }
    finally { setSyncing(null); }
  }
  const sync = (harness: string) => syncAgents(harness).catch((e) => toastError(harness ? `Could not update ${harnessLabel(harness)}` : "Could not update agents", e));

  if (!inDesktop) return <NeedsDesktop what="Skills" />;

  if (page) return <SkillPage key={page.name ?? "new"} name={page.name} onChanged={changed} onBack={() => { setPage(null); changed(); }} />;

  return <div className="flex h-full min-h-0 flex-col" data-testid="skills-view">
    <header className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-2 border-b px-4 py-2.5">
      <h1 className="font-semibold text-base">Skills</h1>
      <Tabs value={tab} onValueChange={(v) => { setTab(v as Tab); if (v === "discover") setDiscoverSeen(true); }}>
        <TabsList><TabsTab value="yours">Yours</TabsTab><TabsTab value="discover">Discover</TabsTab></TabsList>
      </Tabs>
      <div className="relative ml-auto min-w-48 max-w-80 flex-1">
        <Search aria-hidden className="pointer-events-none absolute top-1/2 left-3 size-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input aria-label={tab === "yours" ? "Search your skills" : "Search skills.sh"} className="[&_input]:ps-8" placeholder={tab === "yours" ? "Search your skills" : "Search skills.sh"}
          value={tab === "yours" ? query : discoverQuery} onChange={(e) => (tab === "yours" ? setQuery : setDiscoverQuery)(e.target.value)} />
      </div>
      <Button size="sm" variant="ghost" aria-label="Refresh skills" onClick={changed}><RefreshCw /></Button>
      <Menu>
        <MenuTrigger render={<Button size="sm" />}><Plus data-icon="inline-start" />Add<ChevronDown data-icon="inline-end" /></MenuTrigger>
        <MenuPopup align="end">
          <MenuItem onClick={() => setPage({ name: null })}><Plus />New skill</MenuItem>
          <MenuItem onClick={() => setImporting(true)}><Download />Import from your agents</MenuItem>
        </MenuPopup>
      </Menu>
    </header>

    {tab === "yours" && <>
      <HarnessStatus states={states} error={statesError} syncing={syncing} onSync={(h) => void sync(h)} onRetry={() => void loadStates()} />
      <div className="min-h-0 flex-1 overflow-auto p-4">
        <div className="mx-auto w-full max-w-5xl">
          {listError && <p role="alert" className="mb-3 text-destructive-foreground text-sm">{listError} <button className="underline" onClick={() => void loadList()}>Retry</button></p>}
          {!list && !listError && <div role="status" className="flex items-center gap-2 text-muted-foreground"><Spinner className="size-4" />Loading skills…</div>}
          {list && list.length === 0 && <Empty><EmptyHeader><EmptyTitle>No skills yet</EmptyTitle><EmptyDescription>Find skills in Discover, write your own, or import the ones your agents already have.</EmptyDescription></EmptyHeader><Button onClick={() => { setTab("discover"); setDiscoverSeen(true); }}>Browse Discover</Button></Empty>}
          {list && list.length > 0 && shown.length === 0 && <p className="text-muted-foreground">No skill matches &ldquo;{query}&rdquo;.</p>}
          <ul aria-label="Your skills" className="flex flex-col gap-2">
            {shown.map((s) => {
              const added = timeAgo(s.addedAt);
              const fresh = s.addedAt > 0 && Date.now() / 1000 - s.addedAt < NEW_WINDOW_S;
              return <li key={s.name} className="flex items-center gap-3 rounded-xl border bg-card p-3 hover:bg-accent/40">
                <span aria-hidden className="flex size-11 shrink-0 items-center justify-center rounded-lg border bg-muted/40"><ScrollText className="size-5" strokeWidth={1.5} /></span>
                <button className="min-w-0 flex-1 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring" onClick={() => setPage({ name: s.name })}>
                  <span className="flex items-center gap-2"><span className="truncate font-medium">{s.name}</span>{fresh && <Badge variant="info" size="sm">New</Badge>}</span>
                  <span className="line-clamp-1 text-muted-foreground text-xs">{s.description || "No description"}</span>
                </button>
                <SourceBadge source={s.source} className="hidden sm:inline-flex" />
                <span className="hidden w-28 shrink-0 text-right text-muted-foreground text-xs md:block">{added ? `Added ${added}` : ""}</span>
                <Switch aria-label={`${s.enabled ? "Disable" : "Enable"} ${s.name}`} checked={s.enabled} disabled={toggling.has(s.name)} onCheckedChange={(v) => void toggle(s, v)} />
              </li>;
            })}
          </ul>
        </div>
      </div>
    </>}

    {discoverSeen && <div className={tab === "discover" ? "flex min-h-0 flex-1 flex-col" : "hidden"}>
      <DiscoverTab query={discoverQuery} added={addedFromHub(list ?? [])} canUpdateAgents={(states ?? []).some((s) => s.available)}
        onAdded={changed} onUpdateAgents={() => syncAgents("")} />
    </div>}

    <ImportDialog open={importing} onOpenChange={setImporting} onImported={changed} />
  </div>;
}
