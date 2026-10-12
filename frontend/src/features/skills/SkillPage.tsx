import { useEffect, useRef, useState } from "react";
import { ArrowLeft, ScrollText, Trash2, User } from "lucide-react";
import { Skills, type Skill } from "@/mygo";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsList, TabsTab } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { ConfirmDialog, errText, toastError } from "@/features/projects/dialogs";
import { SourceBadge } from "./SourceBadge";
import { draftProblem, isSlug, retemplate, slugify, sourceInfo, timeAgo } from "./skills";

const NEW_WINDOW_S = 2 * 86400;

/**
 * One skill on its own page: Overview (what it is and where it came from) and Contents (the
 * SKILL.md and its files, editable). `name` null starts a new skill, which opens on Contents.
 */
export function SkillPage({ name, onBack, onChanged }: { name: string | null; onBack: () => void; onChanged: () => void }) {
  const [current, setCurrent] = useState<string | null>(name);
  const [skill, setSkill] = useState<Skill | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [newName, setNewName] = useState("");
  const [body, setBody] = useState("");
  const [tab, setTab] = useState<"overview" | "contents">(name === null ? "contents" : "overview");
  const [saving, setSaving] = useState(false);
  const [toggling, setToggling] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [leaving, setLeaving] = useState(false);
  const loaded = useRef<string | null>(null);

  useEffect(() => {
    if (current === null || loaded.current === current) return;
    setSkill(null); setLoadError(null);
    let live = true;
    Skills.get(current).then((s) => { if (live) { loaded.current = s.name; setSkill(s); setBody(s.body); } }, (e) => { if (live) setLoadError(errText(e)); });
    return () => { live = false; };
  }, [current]);

  const isNew = current === null;
  const dirty = isNew ? body !== "" : skill !== null && body !== skill.body;
  const slug = slugify(newName);
  const problem = isNew || skill ? draftProblem(isNew ? slug : skill!.name, body) : null;
  const canSave = !saving && dirty && problem === null && (!isNew || isSlug(slug));

  async function save() {
    setSaving(true);
    try {
      const saved = await Skills.save(isNew ? slug : skill!.name, body);
      loaded.current = saved.name;
      setSkill(saved); setBody(saved.body); setCurrent(saved.name);
      onChanged();
    } catch (e) { toastError("Could not save skill", e); }
    finally { setSaving(false); }
  }
  async function toggle(enabled: boolean) {
    if (!skill) return;
    setToggling(true);
    try {
      const next = await Skills.setEnabled(skill.name, enabled);
      setSkill((s) => (s ? { ...s, enabled: next.enabled, updatedAt: next.updatedAt } : s));
      onChanged();
    } catch (e) { toastError(`Could not ${enabled ? "enable" : "disable"} ${skill.name}`, e); }
    finally { setToggling(false); }
  }
  async function remove() {
    if (!skill) return;
    await Skills.delete(skill.name);
    onChanged();
    onBack();
  }
  const back = () => { if (dirty) setLeaving(true); else onBack(); };

  const title = isNew ? "New skill" : skill?.name ?? current ?? "";
  const info = skill ? sourceInfo(skill.source) : null;
  const added = skill ? timeAgo(skill.addedAt) : "";
  const edited = skill && skill.updatedAt - skill.addedAt > 60 ? timeAgo(skill.updatedAt) : "";
  const fresh = skill !== null && skill.addedAt > 0 && Date.now() / 1000 - skill.addedAt < NEW_WINDOW_S;

  return <div className="flex h-full min-h-0 flex-col overflow-auto" data-testid="skill-page">
    <div className="mx-auto flex w-full max-w-5xl flex-col gap-6 p-6">
      <Button size="sm" variant="ghost" className="-ml-2 self-start" onClick={back}><ArrowLeft data-icon="inline-start" />Your skills</Button>

      {loadError && <p role="alert" className="text-destructive-foreground text-sm">{loadError}</p>}
      {!isNew && !skill && !loadError && <div role="status" className="flex items-center gap-2 text-muted-foreground"><Spinner className="size-4" />Loading skill…</div>}

      {(isNew || skill) && <>
        <header className="flex flex-wrap items-center gap-4">
          <span aria-hidden className="flex size-16 shrink-0 items-center justify-center rounded-2xl border bg-muted/40"><ScrollText className="size-7" strokeWidth={1.5} /></span>
          <div className="min-w-0 flex-1">
            {isNew
              ? <Input aria-label="Skill name" className="max-w-72" placeholder="skill-name" aria-invalid={newName !== "" && !isSlug(slug) || undefined} value={newName} onChange={(e) => { setNewName(e.target.value); setBody((b) => retemplate(b, slugify(newName), slugify(e.target.value))); }} />
              : <div className="flex items-center gap-2"><h1 className="truncate font-semibold text-xl">{title}</h1>{fresh && <Badge variant="info" size="sm">New</Badge>}</div>}
            <p className="mt-1 truncate text-muted-foreground text-sm">
              {isNew ? <>The directory name is <code className="font-mono">{slug || "…"}</code>. A name in the frontmatter takes precedence.</>
                : <>by {info!.creator}{info!.detail ? ` · ${info!.detail}` : ""}{added && ` · added ${added}`}{edited && ` · updated ${edited}`}</>}
            </p>
          </div>
          {skill && <div className="flex items-center gap-3">
            <label className="flex items-center gap-2 text-sm">{skill.enabled ? "Enabled" : "Disabled"}
              <Switch aria-label={`${skill.enabled ? "Disable" : "Enable"} ${skill.name}`} checked={skill.enabled} disabled={toggling} onCheckedChange={(v) => void toggle(v)} /></label>
            <Button size="sm" variant="outline" onClick={() => setDeleting(true)}><Trash2 data-icon="inline-start" />Delete</Button>
          </div>}
        </header>

        <Tabs value={tab} onValueChange={(v) => setTab(v as "overview" | "contents")}>
          <TabsList variant="underline" className="w-full justify-start border-b">
            {!isNew && <TabsTab value="overview">Overview</TabsTab>}
            <TabsTab value="contents">Contents{skill ? ` · ${1 + (skill.files?.length ?? 0)}` : ""}</TabsTab>
          </TabsList>
        </Tabs>

        {tab === "overview" && skill && info && <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_20rem]">
          <section aria-label="Description">
            <h2 className="mb-2 text-muted-foreground text-sm">Description</h2>
            <p className="max-w-prose text-base leading-relaxed">{skill.description || "No description"}</p>
          </section>
          <aside aria-label="Details" className="flex flex-col gap-5 self-start rounded-2xl border bg-card p-5">
            <div><h3 className="mb-2 text-muted-foreground text-sm">Created by</h3>
              <p className="flex items-center gap-2"><span aria-hidden className="flex size-7 items-center justify-center rounded-md bg-muted"><User className="size-3.5" /></span>{info.creator}{info.detail && <span className="truncate text-muted-foreground text-sm">· {info.detail}</span>}</p></div>
            <div><h3 className="mb-2 text-muted-foreground text-sm">Source</h3><SourceBadge source={skill.source} /></div>
            <div><h3 className="mb-2 text-muted-foreground text-sm">Added</h3><p>{skill.addedAt ? new Date(skill.addedAt * 1000).toLocaleDateString(undefined, { dateStyle: "medium" }) : "Unknown"}{added && <span className="text-muted-foreground"> · {added}</span>}</p></div>
          </aside>
        </div>}

        {tab === "contents" && <section aria-label="Contents" className="flex flex-col gap-3">
          <Textarea aria-label="SKILL.md" disabled={saving} className="font-mono" spellCheck={false} value={body} onChange={(e) => setBody(e.target.value)} />
          {problem && body !== "" && <p role="status" className="text-warning-foreground text-xs">{problem}</p>}
          {skill && (skill.files?.length ?? 0) > 0 && <div><h3 className="mb-1 font-medium text-xs">Resource files</h3><ul className="font-mono text-muted-foreground text-xs">{skill.files.map((f) => <li key={f} className="selectable">{f}</li>)}</ul></div>}
          <div className="flex items-center gap-2">
            <Button disabled={!canSave} onClick={() => void save()}>{saving ? "Saving…" : "Save"}</Button>
            {dirty && <><Badge variant="warning" size="sm">Unsaved</Badge>{!isNew && skill && <Button variant="ghost" disabled={saving} onClick={() => setBody(skill.body)}>Revert</Button>}</>}
          </div>
        </section>}
      </>}
    </div>

    <ConfirmDialog open={deleting} onOpenChange={setDeleting} title={`Delete ${skill?.name ?? "skill"}?`} confirmLabel="Delete skill" onConfirm={remove}>
      Removes the skill from Orole, so no agent sees it. Copies in your agents&rsquo; own folders are removed the next time you update agents.
    </ConfirmDialog>
    <ConfirmDialog open={leaving} onOpenChange={setLeaving} title="Discard unsaved changes?" confirmLabel="Discard changes" onConfirm={async () => { onBack(); }}>
      The skill you are editing has changes that have not been saved.
    </ConfirmDialog>
  </div>;
}
