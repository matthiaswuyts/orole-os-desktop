import { useEffect, useMemo, useState } from "react";
import { ExternalLink, MessageSquare, Pencil, Plus } from "lucide-react";
import { GitHub, type Issue, type IssueDetail } from "@/mygo";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Select, SelectGroup, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { AlertDialog, AlertDialogClose, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogPopup, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Dialog, DialogDescription, DialogFooter, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { cn } from "@/lib/utils";
import { Markdown } from "./Markdown";
import { ErrorBox, EmptyLine, Header, IssueStateIcon, LabelChip, Loading, RefreshButton, agoIso, useAsync } from "./shared";
import { errMsg, useGitHub } from "./store";
import { IssueAgentWorkflow } from "./IssueAgentWorkflow";

const STATES = ["open", "closed", "all"] as const;

export function NoRepo({ what }: { what: string }) {
  return <EmptyLine>Pick a repository on the Repositories tab (or in the selector above) to see its {what}.</EmptyLine>;
}

export function IssuesTab() {
  const { repo, pending, clearPending } = useGitHub();
  const [state, setState] = useState<(typeof STATES)[number]>("open");
  const [label, setLabel] = useState("");
  const [text, setText] = useState("");
  const [sel, setSel] = useState<number | null>(null);
  const [editing, setEditing] = useState<Issue | "new" | null>(null);
  const [limit, setLimit] = useState(50);

  useEffect(() => { setSel(null); setLabel(""); setLimit(50); }, [repo]);
  useEffect(() => {
    if (pending?.kind === "issue" && pending.repo === repo) { setSel(pending.number); clearPending(); }
  }, [pending, repo, clearPending]);

  const list = useAsync(
    () => (repo ? GitHub.issues({ repo, state, labels: label ? [label] : [], assignee: "", creator: "", milestone: "", sort: "updated", limit }) : Promise.resolve([])),
    [repo, state, label, limit],
  );
  const labels = useAsync(() => (repo ? GitHub.labels(repo) : Promise.resolve([])), [repo]);
  const shown = useMemo(() => {
    const t = text.trim().toLowerCase();
    return (list.data ?? []).filter((i) => !t || i.title.toLowerCase().includes(t) || String(i.number) === t.replace("#", ""));
  }, [list.data, text]);

  if (!repo) return <NoRepo what="issues" />;
  return (
    <div className="flex min-h-0 flex-1">
      <section aria-label="Issues" className="flex w-[26rem] shrink-0 flex-col border-r">
        <Header title="Issues" count={list.data && <Badge size="sm" variant="outline">{shown.length}</Badge>}>
          <RefreshButton a={list} />
          <Button size="xs" onClick={() => setEditing("new")}><Plus />New issue</Button>
        </Header>
        <div className="flex flex-wrap items-center gap-1.5 border-b px-2 py-1.5">
          <div role="group" aria-label="State" className="flex rounded-md border">
            {STATES.map((s) => <button key={s} aria-pressed={state === s} onClick={() => setState(s)} className={cn("px-2 py-0.5 text-xs capitalize first:rounded-l-md last:rounded-r-md", state === s ? "bg-accent" : "hover:bg-accent/50")}>{s}</button>)}
          </div>
          <Select value={label} onValueChange={(v) => setLabel(v ?? "")}>
            <SelectTrigger size="sm" aria-label="Filter by label" className="max-w-36"><SelectValue>{label || "All labels"}</SelectValue></SelectTrigger>
            <SelectPopup><SelectGroup>
              <SelectItem value="">All labels</SelectItem>
              {labels.data?.map((l) => <SelectItem key={l.id} value={l.name}>{l.name}</SelectItem>)}
            </SelectGroup></SelectPopup>
          </Select>
          <Input size="sm" type="search" value={text} onChange={(e) => setText(e.target.value)} placeholder="Filter loaded…" aria-label="Filter issues" className="min-w-24 flex-1" />
        </div>
        <div className="min-h-0 flex-1 overflow-auto">
          {list.error && <ErrorBox error={list.error} onRetry={list.reload} />}
          {list.loading && !list.data && <Loading what="issues" />}
          {list.data && shown.length === 0 && <EmptyLine>No {state === "all" ? "" : state} issues{text || label ? " match these filters" : ""}.</EmptyLine>}
          <ul>
            {shown.map((i) => (
              <li key={i.id}>
                <button onClick={() => setSel(i.number)} aria-current={sel === i.number ? "true" : undefined} className={cn("flex w-full gap-2 border-b px-3 py-2 text-left outline-none hover:bg-accent/50 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset", sel === i.number && "bg-accent")}>
                  <span className="mt-0.5"><IssueStateIcon issue={i} /></span>
                  <span className="min-w-0 flex-1">
                    <span className="block text-sm">{i.title}</span>
                    {i.labels.length > 0 && <span className="mt-1 flex flex-wrap gap-1">{i.labels.map((l) => <LabelChip key={l.id} label={l} />)}</span>}
                    <span className="mt-0.5 flex items-center gap-2 text-[11px] text-muted-foreground">
                      #{i.number} · {i.user.login} · {agoIso(i.updated_at)}
                      {i.comments > 0 && <span className="inline-flex items-center gap-0.5"><MessageSquare className="size-3" />{i.comments}</span>}
                    </span>
                  </span>
                </button>
              </li>
            ))}
          </ul>
          {list.data && list.data.length >= limit && <div className="p-2 text-center"><Button size="xs" variant="outline" onClick={() => setLimit((l) => l + 50)}>Load more</Button></div>}
        </div>
      </section>
      <div className="min-w-0 flex-1 overflow-auto">
        {sel === null ? <EmptyLine>Select an issue to read it.</EmptyLine> : <IssueDetailPane key={`${repo}#${sel}`} repo={repo} number={sel} onEdit={(i) => setEditing(i)} />}
      </div>
      {editing && <IssueDialog repo={repo} issue={editing === "new" ? null : editing} onClose={() => setEditing(null)} onSaved={(i) => { setEditing(null); setSel(i.number); list.reload(); }} />}
    </div>
  );
}

function IssueDetailPane({ repo, number, onEdit }: { repo: string; number: number; onEdit: (i: Issue) => void }) {
  const a = useAsync(() => GitHub.issue(repo, number), [repo, number]);
  const [confirm, setConfirm] = useState<"close" | "reopen" | "comment" | null>(null);
  const [comment, setComment] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  if (a.error && !a.data) return <ErrorBox error={a.error} onRetry={a.reload} />;
  if (!a.data) return <Loading what="issue" />;
  const { issue, comments } = a.data as IssueDetail;
  const open = issue.state === "open";

  const doConfirmed = () => {
    setBusy(true); setErr(null);
    const p = confirm === "comment"
      ? GitHub.comment(repo, number, comment, true).then(() => setComment(""))
      : GitHub.updateIssue({ repo, number, title: null, body: null, state: confirm === "close" ? "closed" : "open", labels: [], setLabels: false, assignees: [], setAssignees: false, milestone: null, confirmed: true }).then(() => undefined);
    p.then(() => setConfirm(null), (e) => setErr(errMsg(e))).finally(() => setBusy(false));
  };

  return (
    <article className="mx-auto max-w-3xl p-4">
      <header className="flex items-start gap-2">
        <h2 className="flex-1 break-words font-semibold text-lg leading-snug">{issue.title} <span className="font-normal text-muted-foreground">#{issue.number}</span></h2>
        <Button size="xs" variant="outline" onClick={() => onEdit(issue)}><Pencil />Edit</Button>
        <a href={issue.html_url} target="_blank" rel="noopener noreferrer" aria-label="Open on GitHub" className="inline-flex size-6 items-center justify-center rounded-md hover:bg-accent"><ExternalLink className="size-3.5" /></a>
      </header>
      <div className="mt-1 flex flex-wrap items-center gap-2 text-muted-foreground text-xs">
        <Badge variant={open ? "success" : "info"}>{open ? "Open" : issue.state_reason === "not_planned" ? "Closed (not planned)" : "Closed"}</Badge>
        <span>{issue.user.login} opened {agoIso(issue.created_at)}</span>
        {issue.assignees.length > 0 && <span>assigned to {issue.assignees.map((u) => u.login).join(", ")}</span>}
        {issue.milestone && <Badge size="sm" variant="outline">{issue.milestone.title}</Badge>}
      </div>
      {issue.labels.length > 0 && <div className="mt-2 flex flex-wrap gap-1">{issue.labels.map((l) => <LabelChip key={l.id} label={l} />)}</div>}
      <div className="mt-3 rounded-lg border bg-card p-3"><Markdown source={issue.body} /></div>
      <IssueAgentWorkflow key={`${repo}#${issue.number}`} repo={repo} issue={issue} />
      <h3 className="mt-4 mb-1 font-medium text-[13px]">{comments.length} {comments.length === 1 ? "comment" : "comments"}</h3>
      <ul className="space-y-2">
        {comments.map((c) => (
          <li key={c.id} className="rounded-lg border bg-card">
            <p className="border-b px-3 py-1 text-muted-foreground text-xs"><strong className="text-foreground">{c.user.login}</strong> · {agoIso(c.created_at)}{c.author_association !== "NONE" && ` · ${c.author_association.toLowerCase()}`}</p>
            <div className="p-3"><Markdown source={c.body} /></div>
          </li>
        ))}
      </ul>
      <div className="mt-3 flex flex-col gap-2">
        <Textarea aria-label="New comment" value={comment} onChange={(e) => setComment(e.target.value)} placeholder="Leave a comment (markdown)" className="min-h-20" />
        <div className="flex justify-end gap-2">
          <Button size="sm" variant="outline" onClick={() => setConfirm(open ? "close" : "reopen")}>{open ? "Close issue" : "Reopen issue"}</Button>
          <Button size="sm" disabled={!comment.trim()} onClick={() => setConfirm("comment")}>Comment…</Button>
        </div>
      </div>
      <AlertDialog open={confirm !== null} onOpenChange={(o) => { if (!o && !busy) { setConfirm(null); setErr(null); } }}>
        <AlertDialogPopup>
          <AlertDialogHeader>
            <AlertDialogTitle>{confirm === "comment" ? "Post this comment?" : confirm === "close" ? "Close this issue?" : "Reopen this issue?"}</AlertDialogTitle>
            <AlertDialogDescription render={<div />}>This acts on GitHub as you, on <span className="font-mono">{repo}#{number}</span>, and is visible to everyone with access to the repository.</AlertDialogDescription>
          </AlertDialogHeader>
          {err && <p role="alert" className="px-6 pb-4 text-destructive-foreground text-sm">{err}</p>}
          <AlertDialogFooter>
            <AlertDialogClose render={<Button variant="outline" disabled={busy} />}>Cancel</AlertDialogClose>
            <Button disabled={busy} onClick={doConfirmed}>{busy ? "Working…" : "Confirm"}</Button>
          </AlertDialogFooter>
        </AlertDialogPopup>
      </AlertDialog>
    </article>
  );
}

/** Create or edit an issue. Submitting is the explicit confirmation sent to the daemon. */
function IssueDialog({ repo, issue, onClose, onSaved }: { repo: string; issue: Issue | null; onClose: () => void; onSaved: (i: Issue) => void }) {
  const [title, setTitle] = useState(issue?.title ?? "");
  const [body, setBody] = useState(issue?.body ?? "");
  const [labels, setLabels] = useState<string[]>(issue?.labels.map((l) => l.name) ?? []);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const all = useAsync(() => GitHub.labels(repo), [repo]);

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true); setErr(null);
    const w = { repo, number: issue?.number ?? 0, title, body, state: null, labels, setLabels: true, assignees: [], setAssignees: false, milestone: null, confirmed: true };
    (issue ? GitHub.updateIssue(w) : GitHub.createIssue(w)).then(onSaved, (x) => setErr(errMsg(x))).finally(() => setBusy(false));
  };
  const toggle = (n: string) => setLabels((l) => (l.includes(n) ? l.filter((x) => x !== n) : [...l, n]));
  return (
    <Dialog open onOpenChange={(o) => { if (!o && !busy) onClose(); }}>
      <DialogPopup>
        <form onSubmit={submit} className="contents">
          <DialogHeader>
            <DialogTitle>{issue ? `Edit #${issue.number}` : "New issue"} in {repo}</DialogTitle>
            <DialogDescription>Saving posts to GitHub as you and is visible to everyone with access.</DialogDescription>
          </DialogHeader>
          <DialogPanel className="flex flex-col gap-3">
            <Field><FieldLabel>Title</FieldLabel><Input autoFocus value={title} onChange={(e) => setTitle(e.target.value)} /></Field>
            <Field><FieldLabel>Description</FieldLabel><Textarea value={body} onChange={(e) => setBody(e.target.value)} className="min-h-32 font-mono" /><FieldDescription>Markdown.</FieldDescription></Field>
            <fieldset><legend className="mb-1 text-sm">Labels</legend>
              <div className="flex flex-wrap gap-1">
                {all.data?.map((l) => <button type="button" key={l.id} aria-pressed={labels.includes(l.name)} onClick={() => toggle(l.name)} className={cn("rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring", !labels.includes(l.name) && "opacity-50")}><LabelChip label={l} /></button>)}
                {all.loading && !all.data && <span className="text-muted-foreground text-xs">Loading labels…</span>}
                {all.data?.length === 0 && <span className="text-muted-foreground text-xs">This repository has no labels.</span>}
              </div>
            </fieldset>
            {err && <p role="alert" className="text-destructive-foreground text-sm">{err}</p>}
          </DialogPanel>
          <DialogFooter>
            <Button type="button" variant="outline" disabled={busy} onClick={onClose}>Cancel</Button>
            <Button type="submit" disabled={busy || !title.trim()}>{busy ? "Saving…" : issue ? "Save to GitHub" : "Create on GitHub"}</Button>
          </DialogFooter>
        </form>
      </DialogPopup>
    </Dialog>
  );
}
