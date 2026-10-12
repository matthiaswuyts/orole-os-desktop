import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ArrowUpDown, Check, Download, Plus, ScrollText, User } from "lucide-react";
import { Skills, type HubEntry, type HubTopic, type Skill } from "@/mygo";
import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Menu, MenuPopup, MenuRadioGroup, MenuRadioItem, MenuTrigger } from "@/components/ui/menu";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { errText } from "@/features/projects/dialogs";
import { formatInstalls, hubKey } from "./skills";
import { SkillPreviewDialog, type HubTarget } from "./SkillPreviewDialog";

/** Cards revealed at a time. skills.sh pages hold well over a hundred skills; descriptions are fetched only for revealed ones. */
const CHUNK = 24;

type View = "all-time" | "trending";
const VIEWS: { id: View; label: string }[] = [{ id: "all-time", label: "Most installed" }, { id: "trending", label: "Trending" }];

type Listing = { entries: HubEntry[]; page: number; hasMore: boolean; loading: boolean; error: string | null };
const EMPTY: Listing = { entries: [], page: 0, hasMore: false, loading: true, error: null };

const SEARCH_DELAY_MS = 350;

const Tile = ({ className }: { className?: string }) => <span aria-hidden className={cn("flex shrink-0 items-center justify-center rounded-xl border bg-muted/40", className)}><ScrollText className="size-1/2" strokeWidth={1.5} /></span>;

/**
 * Browse and search the public skills.sh catalogue. Cards show what skills.sh lists (name, author,
 * installs) at once; descriptions are read afterwards and fill in. Topics are skills.sh's own
 * categories. A skill is added from its preview, never straight from the grid.
 */
export function DiscoverTab({ query, added, canUpdateAgents, onAdded, onUpdateAgents }: {
  query: string;
  added: ReadonlySet<string>;
  canUpdateAgents: boolean;
  onAdded: (skill: Skill) => void;
  onUpdateAgents: () => Promise<void>;
}) {
  const [view, setView] = useState<View>("all-time");
  const [topic, setTopic] = useState<string | null>(null);
  const [topics, setTopics] = useState<HubTopic[]>([]);
  const [listing, setListing] = useState<Listing>(EMPTY);
  const [descriptions, setDescriptions] = useState<Record<string, string>>({});
  const [target, setTarget] = useState<HubTarget | null>(null);
  const [revealed, setRevealed] = useState(CHUNK);
  const request = useRef(0);
  const asked = useRef(new Set<string>());
  const text = query.trim();
  const searching = [...text].length >= 2;
  const topicLabel = topics.find((t) => t.id === topic)?.label;

  useEffect(() => { Skills.hubTopics().then(setTopics, () => setTopics([])); }, []);

  const describe = useCallback(async (entries: HubEntry[]) => {
    const refs = entries.filter((e) => !asked.current.has(hubKey(e))).map((e) => ({ source: e.source, skillId: e.skillId }));
    for (const ref of refs) asked.current.add(hubKey(ref));
    if (refs.length === 0) return;
    try {
      const found = await Skills.hubDescribe(refs);
      setDescriptions((current) => ({ ...current, ...Object.fromEntries(found.filter((d) => d.description).map((d) => [hubKey(d), d.description])) }));
    } catch { /* descriptions are a nicety; the cards work without them */ }
  }, []);

  const load = useCallback(async (page: number, reset: boolean) => {
    const id = ++request.current;
    if (reset) setRevealed(CHUNK);
    setListing((current) => ({ ...(reset ? EMPTY : current), loading: true, error: null }));
    try {
      const result = searching ? await Skills.hubSearch(text) : topic ? await Skills.hubTopic(topic) : await Skills.hubBrowse(view, page);
      if (id !== request.current) return;
      setListing((current) => {
        const seen = new Set(reset ? [] : current.entries.map(hubKey));
        const fresh = result.skills.filter((e) => !seen.has(hubKey(e)));
        return { entries: reset ? result.skills : [...current.entries, ...fresh], page: result.page, hasMore: result.hasMore, loading: false, error: null };
      });
    } catch (e) {
      if (id === request.current) setListing((current) => ({ ...current, loading: false, error: errText(e) }));
    }
  }, [searching, text, topic, view]);

  useEffect(() => {
    const timer = setTimeout(() => void load(0, true), searching ? SEARCH_DELAY_MS : 0);
    return () => { clearTimeout(timer); request.current++; };
  }, [load, searching]);

  const browsing = !searching && topic === null;
  const hero = useMemo(
    () => (browsing && view === "all-time" ? listing.entries.find((e) => e.official && !added.has(hubKey(e))) : undefined),
    [browsing, view, listing.entries, added],
  );
  const everything = useMemo(() => listing.entries.filter((e) => e !== hero), [listing.entries, hero]);
  const grid = useMemo(() => (searching ? everything : everything.slice(0, revealed)), [searching, everything, revealed]);
  useEffect(() => { void describe(hero ? [hero, ...grid] : grid); }, [hero, grid, describe]);
  const open = (e: HubEntry) => setTarget({ source: e.source, skillId: e.skillId, name: e.name });
  const title = searching ? `Results for “${text}”` : topicLabel ?? VIEWS.find((v) => v.id === view)!.label;
  const chip = (active: boolean) => cn("rounded-full border px-3 py-1 text-sm outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50",
    active ? "border-foreground bg-foreground text-background" : "bg-card text-muted-foreground hover:bg-accent hover:text-foreground");

  return <div className="flex min-h-0 flex-1 flex-col overflow-auto p-4" data-testid="skills-discover">
    <div className="mx-auto flex w-full max-w-5xl flex-col">
      {topics.length > 0 && <div role="group" aria-label="Browse by topic" className="mb-5 flex flex-wrap gap-2">
        <button aria-pressed={topic === null} disabled={searching} className={chip(topic === null && !searching)} onClick={() => setTopic(null)}>All</button>
        {topics.map((t) => <button key={t.id} aria-pressed={topic === t.id} disabled={searching} className={chip(topic === t.id && !searching)} onClick={() => setTopic(t.id)}>{t.label}</button>)}
      </div>}

      {hero && <section aria-label="Featured skill" className="mb-6 flex items-center gap-5 rounded-2xl border bg-card p-5">
        <Tile className="hidden size-16 sm:flex" />
        <div className="min-w-0 flex-1">
          <p className="flex items-center gap-1.5 text-muted-foreground text-xs"><User aria-hidden className="size-3" />{hero.source.split("/")[0]} · Featured</p>
          <h2 className="mt-1 truncate font-semibold text-xl">{hero.name}</h2>
          {descriptions[hubKey(hero)] && <p className="mt-1 line-clamp-2 max-w-prose text-muted-foreground">{descriptions[hubKey(hero)]}</p>}
        </div>
        <Button onClick={() => open(hero)}>Add</Button>
      </section>}

      <div className="mb-3 flex items-center gap-2">
        <h2 className="font-medium text-base">{title}</h2>
        {browsing && <Menu>
          <MenuTrigger render={<Button size="icon-xs" variant="ghost" className="ml-auto" aria-label="Sort skills" />}><ArrowUpDown /></MenuTrigger>
          <MenuPopup align="end">
            <MenuRadioGroup value={view} onValueChange={(v) => setView(v as View)}>
              {VIEWS.map((v) => <MenuRadioItem key={v.id} value={v.id}>{v.label}</MenuRadioItem>)}
            </MenuRadioGroup>
          </MenuPopup>
        </Menu>}
      </div>

      {listing.error && <div role="alert" className="mb-3 rounded-lg border border-destructive/40 p-3 text-sm">
        <p className="font-medium">Couldn&rsquo;t reach skills.sh</p>
        <p className="break-words text-muted-foreground text-xs">{listing.error}</p>
        <Button size="xs" variant="outline" className="mt-2" onClick={() => void (listing.entries.length === 0 ? load(0, true) : load(listing.page + 1, false))}>Retry</Button>
      </div>}

      {listing.loading && listing.entries.length === 0 && <ul aria-label="Loading skills" className="grid grid-cols-1 gap-3 lg:grid-cols-2">
        {Array.from({ length: 6 }, (_, i) => <li key={i}><Skeleton className="h-24 rounded-xl" /></li>)}
      </ul>}
      {!listing.loading && !listing.error && grid.length === 0 && !hero && <Empty><EmptyHeader><EmptyTitle>No skills found</EmptyTitle><EmptyDescription>{searching ? `Nothing on skills.sh matches “${text}”.` : "skills.sh returned no skills."}</EmptyDescription></EmptyHeader></Empty>}

      <ul className="grid grid-cols-1 gap-3 lg:grid-cols-2">
        {grid.map((entry) => {
          const isAdded = added.has(hubKey(entry));
          const description = descriptions[hubKey(entry)];
          return <li key={hubKey(entry)} className="relative flex items-start gap-3 rounded-xl border bg-card p-3 hover:bg-accent/40">
            <Tile className="size-11" />
            <div className="min-w-0 flex-1">
              <button className="block max-w-full truncate text-left font-medium outline-none after:absolute after:inset-0 after:rounded-xl focus-visible:after:ring-2 focus-visible:after:ring-ring" aria-label={`${isAdded ? "View" : "Add"} ${entry.name}`} onClick={() => open(entry)}>{entry.name}</button>
              {description ? <p className="line-clamp-2 text-muted-foreground text-xs">{description}</p> : <Skeleton aria-hidden className="mt-1 h-3 w-3/4" />}
              <p className="mt-1.5 flex items-center gap-3 text-muted-foreground text-xs">
                <span className="flex min-w-0 items-center gap-1"><User aria-hidden className="size-3 shrink-0" /><span className="truncate">{entry.source.split("/")[0]}</span></span>
                {entry.installs > 0 && <span className="flex shrink-0 items-center gap-1"><Download aria-hidden className="size-3" />{formatInstalls(entry.installs)}<span className="sr-only"> installs</span></span>}
              </p>
            </div>
            <span aria-hidden className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-muted">{isAdded ? <Check className="size-4 text-success" /> : <Plus className="size-4" />}</span>
          </li>;
        })}
      </ul>

      {!searching && (revealed < everything.length || listing.hasMore) && <Button variant="outline" className="mt-4 self-center" disabled={listing.loading}
        onClick={() => { setRevealed((n) => n + CHUNK); if (revealed + CHUNK > everything.length && listing.hasMore) void load(listing.page + 1, false); }}>{listing.loading ? "Loading…" : "Show more"}</Button>}
    </div>

    <SkillPreviewDialog target={target} added={target !== null && added.has(`${target.source}/${target.skillId}`)} canUpdateAgents={canUpdateAgents}
      onClose={() => setTarget(null)} onAdded={onAdded} onUpdateAgents={onUpdateAgents} />
  </div>;
}
