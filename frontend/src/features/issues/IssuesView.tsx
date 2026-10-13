import { useEffect, useMemo, useRef, useState } from "react";
import { Bot, CircleAlert, MessageSquare, Search } from "lucide-react";
import { GitHub, type Issue, type Label } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { cn } from "@/lib/utils";
import { useProjects } from "@/stores/projects";
import { Button } from "@/components/ui/button";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
import { Kbd } from "@/components/ui/kbd";
import { Select, SelectGroup, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { NeedsDesktop } from "@/features/shared/States";
import { SetupScreen } from "@/features/github/SetupScreen";
import { EmptyLine, IssueStateIcon, LabelChip, RefreshButton, agoIso, useAsync } from "@/features/github/shared";
import { useGitHub } from "@/features/github/store";
import { IssueDetail } from "./IssueDetail";
import { PersonAvatar } from "./PersonAvatar";
import { loadIssues, matchesQuery, mergeLabels, rankLabels, type IssueState, type Ownership } from "./feed";
import { issueScopes } from "./repos";
import { isShortcutTarget } from "./shortcuts";
import { Checkbox } from "@/components/ui/checkbox";
import { BulkAgentSheet } from "./BulkAgentSheet";

const SCOPE_KEY = "orole.issues.scope";
const STATES: { value: IssueState; label: string }[] = [{ value: "open", label: "Open" }, { value: "closed", label: "Closed" }, { value: "all", label: "Any state" }];
const PAGE = 50;
const MAX_CATALOG_REPOS = 6;
const keyOf = (issue: Pick<Issue, "repo" | "number">) => `${issue.repo}#${issue.number}`;
/** Agents are started one after another, so a selection is capped to keep that run short and reviewable. */
const MAX_SELECTED = 10;
const toggled = (current: string[], key: string) => (current.includes(key) ? current.filter((item) => item !== key) : current.length >= MAX_SELECTED ? current : [...current, key]);

/** GitHub issues across the projects that live on GitHub, with one-step hand-off to an agent. */
export function IssuesView() {
  const { status, error, load, startSync } = useGitHub();
  useEffect(() => { void load(); return startSync(); }, [load, startSync]);

  if (!inDesktop) return <NeedsDesktop what="Issues" />;
  if (!status && !error) return <div role="status" className="flex h-full items-center justify-center gap-2 text-muted-foreground text-sm"><Spinner className="size-4" />Checking GitHub connection…</div>;
  if (!status?.connected) return <div className="flex h-full overflow-auto"><SetupScreen /></div>;
  return <IssueBrowser me={status.user?.login ?? ""} />;
}

function IssueBrowser({ me }: { me: string }) {
  const projects = useProjects((state) => state.projects);
  const scopes = useMemo(() => issueScopes(projects), [projects]);
  const [scopeId, setScopeId] = useState(() => localStorage.getItem(SCOPE_KEY) ?? "all");
  const scope = scopes.find((item) => item.id === scopeId);
  const repos = useMemo(() => (scope ? [scope.repo] : scopes.map((item) => item.repo)), [scope, scopes]);
  const [state, setState] = useState<IssueState>("open");
  const [ownership, setOwnership] = useState<Ownership>("all");
  const [labels, setLabels] = useState<string[]>([]);
  const [query, setQuery] = useState("");
  const [limit, setLimit] = useState(PAGE);
  const [picked, setPicked] = useState<string | null>(null);
  const [checked, setChecked] = useState<string[]>([]);
  const [bulk, setBulk] = useState(false);
  const search = useRef<HTMLInputElement>(null);
  const list = useRef<HTMLUListElement>(null);

  const feed = useAsync(() => loadIssues({ repos, state, labels, ownership, me, limit }), [repos.join(","), state, labels.join("\n"), ownership, me, limit], repos.length > 0);
  const catalog = useAsync(async () => (await Promise.all(repos.map((repo) => GitHub.labels(repo).catch(() => [] as Label[])))).flat(), [repos.join(",")], repos.length > 0 && repos.length <= MAX_CATALOG_REPOS);
  // Keep the previous page on screen while a filter change reloads, so the list does not flash empty.
  const kept = useRef(feed.data);
  useEffect(() => { if (feed.data) kept.current = feed.data; }, [feed.data]);
  const result = feed.data ?? kept.current;
  const issues = useMemo(() => (result?.issues ?? []).filter((issue) => matchesQuery(issue, query)), [result, query]);
  const selected = issues.find((issue) => keyOf(issue) === picked) ?? issues[0];
  const names = useMemo(() => {
    const short = repos.map((repo) => repo.split("/")[1]!);
    return new Set(short.filter((name, index) => short.indexOf(name) !== index));
  }, [repos]);
  const chips = useMemo(() => rankLabels(mergeLabels(catalog.data ?? [], (result?.issues ?? []).flatMap((issue) => issue.labels)), result?.issues ?? [], labels).slice(0, 14), [catalog.data, result, labels]);
  const related = useMemo(() => (result?.issues ?? []).filter((issue) => issue.repo === selected?.repo).map((issue) => ({ number: issue.number, title: issue.title })), [result, selected?.repo]);
  // Selection survives search and filter changes; it only counts issues that are still loaded.
  const chosen = useMemo(() => (result?.issues ?? []).filter((issue) => checked.includes(keyOf(issue))), [result, checked]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (!isShortcutTarget(event)) return;
      if (event.key === "/") { event.preventDefault(); search.current?.focus(); return; }
      if (event.key === "x" && selected) { event.preventDefault(); setChecked((current) => toggled(current, keyOf(selected))); return; }
      const step = event.key === "j" || event.key === "ArrowDown" ? 1 : event.key === "k" || event.key === "ArrowUp" ? -1 : 0;
      if (!step || issues.length === 0) return;
      event.preventDefault();
      const at = Math.max(0, issues.findIndex((issue) => keyOf(issue) === (selected && keyOf(selected))));
      setPicked(keyOf(issues[Math.min(issues.length - 1, Math.max(0, at + step))]!));
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [issues, selected]);
  useEffect(() => { list.current?.querySelector("[aria-current=true]")?.scrollIntoView({ block: "nearest" }); }, [selected && keyOf(selected)]);

  const chooseScope = (id: string) => { localStorage.setItem(SCOPE_KEY, id); setScopeId(id); setPicked(null); setLabels([]); setLimit(PAGE); setChecked([]); };
  const toggleLabel = (name: string) => { setLabels((current) => (current.includes(name) ? current.filter((item) => item !== name) : [...current, name])); setLimit(PAGE); };

  if (scopes.length === 0) return <div className="flex h-full items-center justify-center"><EmptyLine>No project has a GitHub remote yet. Add a project whose <code className="font-mono">origin</code> is on github.com to see its issues here.</EmptyLine></div>;
  return <div className="flex h-full min-h-0 flex-col">
    <div className="flex shrink-0 flex-col gap-2 border-b px-3 py-2">
      <div className="flex flex-wrap items-center gap-2">
        <InputGroup className="h-8 w-64 min-w-40 flex-none">
          <InputGroupAddon><Search aria-hidden /></InputGroupAddon>
          <InputGroupInput ref={search} type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search issues" aria-label="Search issues" onKeyDown={(event) => { if (event.key === "Escape") { if (query) setQuery(""); else event.currentTarget.blur(); } }} />
          <InputGroupAddon align="inline-end"><Kbd>/</Kbd></InputGroupAddon>
        </InputGroup>
        <ToggleGroup aria-label="Whose issues" variant="outline" size="sm" value={[ownership]} onValueChange={(next) => { const value = next[0] as Ownership | undefined; if (value) { setOwnership(value); setPicked(null); setLimit(PAGE); } }}>
          <ToggleGroupItem value="all">All</ToggleGroupItem>
          <ToggleGroupItem value="mine" disabled={!me}>Mine</ToggleGroupItem>
          <ToggleGroupItem value="unassigned">Unassigned</ToggleGroupItem>
        </ToggleGroup>
        <Select value={state} onValueChange={(value) => { if (value) { setState(value as IssueState); setPicked(null); setLimit(PAGE); } }}>
          <SelectTrigger size="sm" aria-label="State" className="w-auto min-w-0"><SelectValue>{STATES.find((item) => item.value === state)?.label}</SelectValue></SelectTrigger>
          <SelectPopup><SelectGroup>{STATES.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}</SelectGroup></SelectPopup>
        </Select>
        <span className="flex-1" />
        <Select value={scope?.id ?? "all"} onValueChange={(value) => { if (value) chooseScope(value); }}>
          <SelectTrigger size="sm" aria-label="Project" className="w-auto min-w-0 max-w-56"><SelectValue>{scope?.name ?? "All projects"}</SelectValue></SelectTrigger>
          <SelectPopup align="end"><SelectGroup>
            <SelectItem value="all">All projects</SelectItem>
            {scopes.map((item) => <SelectItem key={item.id} value={item.id}>{item.name} <span className="text-muted-foreground">{item.repo}</span></SelectItem>)}
          </SelectGroup></SelectPopup>
        </Select>
        <RefreshButton a={feed} />
      </div>
      {chips.length > 0 && <div role="group" aria-label="Labels" className="flex flex-wrap items-center gap-1.5">
        {chips.map((label) => {
          const on = labels.includes(label.name);
          return <button key={label.name} type="button" aria-pressed={on} onClick={() => toggleLabel(label.name)} className={cn("inline-flex items-center rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring", on ? "ring-2 ring-ring" : "opacity-80 hover:opacity-100")}><LabelChip label={label} /></button>;
        })}
        {labels.length > 0 && <Button size="xs" variant="ghost" onClick={() => { setLabels([]); setLimit(PAGE); }}>Clear labels</Button>}
      </div>}
      {!!result?.failures.length && <div role="alert" className="flex items-start gap-2 rounded-md border border-warning/40 bg-warning/8 px-2 py-1.5 text-warning-foreground text-xs">
        <CircleAlert aria-hidden className="mt-0.5 size-3.5 shrink-0" />
        <span className="min-w-0 flex-1 break-words">Could not load {result.failures.map((failure) => `${failure.repo} (${failure.message})`).join("; ")}.</span>
        <Button size="xs" variant="ghost" onClick={feed.reload}>Retry</Button>
      </div>}
    </div>
    <div className="flex min-h-0 flex-1">
      <section aria-label="Issue list" className="flex w-[26rem] max-w-[45%] shrink-0 flex-col border-r">
        <div className="min-h-0 flex-1 overflow-auto">
          {feed.loading && !result && <div role="status" className="flex items-center justify-center gap-2 p-6 text-muted-foreground text-sm"><Spinner className="size-4" />Loading issues…</div>}
          {result && issues.length === 0 && <EmptyLine>{emptyText(query, labels, ownership, state, result.issues.length > 0)}</EmptyLine>}
          <ul ref={list} className="flex flex-col gap-0.5 p-2">
            {issues.map((issue) => {
              const key = keyOf(issue);
              const active = selected && keyOf(selected) === key;
              const on = checked.includes(key);
              const assignee = issue.assignees[0];
              return <li key={key}>
                <div className={cn("group flex gap-2 rounded-lg px-2 py-2 transition-colors hover:bg-muted/60", active && "bg-muted")}>
                  <Tooltip disabled={on || checked.length < MAX_SELECTED}>
                    <TooltipTrigger render={<span className="relative mt-0.5 size-4 shrink-0" />}>
                      <span aria-hidden className={cn("absolute inset-0 flex items-center justify-center transition-opacity group-hover:opacity-0 group-has-[[data-slot=checkbox]:focus-visible]:opacity-0", (on || chosen.length > 0) && "opacity-0")}><IssueStateIcon issue={issue} /></span>
                      <Checkbox aria-label={`Select ${key}`} checked={on} disabled={!on && checked.length >= MAX_SELECTED}
                        onCheckedChange={() => setChecked((current) => toggled(current, key))}
                        className={cn("absolute inset-0 size-4 opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 data-checked:opacity-100", chosen.length > 0 && "opacity-100")} />
                    </TooltipTrigger>
                    <TooltipPopup>Select up to {MAX_SELECTED} issues</TooltipPopup>
                  </Tooltip>
                  <button type="button" aria-current={active ? "true" : undefined} onClick={() => setPicked(key)} className="flex min-w-0 flex-1 gap-2 rounded-md text-left outline-none focus-visible:ring-2 focus-visible:ring-ring">
                    <span className="min-w-0 flex-1">
                      <span className="line-clamp-2 font-medium text-[13px] leading-snug">{issue.title}</span>
                      <span className="mt-0.5 block truncate text-[11px] text-muted-foreground">{names.has(issue.repo.split("/")[1]!) || repos.length > 1 ? issue.repo : issue.repo.split("/")[1]}#{issue.number} · {agoIso(issue.updated_at)} · {issue.user.login}</span>
                      {issue.labels.length > 0 && <span className="mt-1 flex flex-wrap gap-1">{issue.labels.map((label) => <LabelChip key={label.id} label={label} />)}</span>}
                    </span>
                    <span className="flex shrink-0 flex-col items-end gap-1 text-[11px] text-muted-foreground">
                      {assignee && <Tooltip><TooltipTrigger render={<span className="inline-flex" />}><PersonAvatar login={assignee.login} className="size-5 text-[10px]" /></TooltipTrigger><TooltipPopup>Assigned to {assignee.login}</TooltipPopup></Tooltip>}
                      {issue.comments > 0 && <span className="inline-flex items-center gap-0.5"><MessageSquare aria-hidden className="size-3" />{issue.comments}</span>}
                    </span>
                  </button>
                </div>
              </li>;
            })}
          </ul>
          {result?.full && <div className="p-2 text-center"><Button size="xs" variant="outline" disabled={feed.loading} onClick={() => setLimit((current) => current + PAGE)}>Load more</Button></div>}
        </div>
        {chosen.length > 0 ? <footer className="flex shrink-0 items-center gap-2 border-t px-3 py-2 text-sm">
          <span className="font-medium tabular-nums">{chosen.length} selected</span>
          <Button size="xs" variant="ghost" onClick={() => setChecked([])}>Clear</Button>
          <span className="flex-1" />
          <Button size="sm" onClick={() => setBulk(true)}><Bot data-icon="inline-start" />Start {chosen.length} {chosen.length === 1 ? "agent" : "agents"}</Button>
        </footer> : <footer className="flex shrink-0 items-center gap-3 border-t px-3 py-1.5 text-[11px] text-muted-foreground">
          <span className="inline-flex items-center gap-1"><Kbd>J</Kbd><Kbd>K</Kbd>move</span>
          <span className="inline-flex items-center gap-1"><Kbd>X</Kbd>select</span>
          <span className="inline-flex items-center gap-1"><Kbd>S</Kbd>start agent</span>
          <span className="inline-flex items-center gap-1"><Kbd>G</Kbd>GitHub</span>
          <span className="ml-auto tabular-nums">{issues.length}{result?.full ? "+" : ""}</span>
        </footer>}
      </section>
      <div className="min-w-0 flex-1 overflow-auto">
        {selected ? <IssueDetail key={keyOf(selected)} repo={selected.repo} number={selected.number} related={related} onChanged={feed.reload} /> : <EmptyLine>Select an issue to read it.</EmptyLine>}
      </div>
    </div>
    {bulk && <BulkAgentSheet issues={chosen} onClose={() => setBulk(false)} onStarted={() => setChecked([])} />}
  </div>;
}

function emptyText(query: string, labels: string[], ownership: Ownership, state: IssueState, loadedSome: boolean): string {
  if (query.trim() && loadedSome) return `No loaded issue matches “${query.trim()}”. Search covers the issues loaded here; load more or narrow the project.`;
  const who = ownership === "mine" ? "assigned to you" : ownership === "unassigned" ? "without an assignee" : "";
  const what = labels.length > 0 ? ` labelled ${labels.join(", ")}` : "";
  return `No ${state === "all" ? "" : `${state} `}issues${who ? ` ${who}` : ""}${what}.`;
}
