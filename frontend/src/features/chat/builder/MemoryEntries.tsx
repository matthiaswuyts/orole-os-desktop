import { useEffect, useId, useState } from "react";
import { Check, Pencil, Plus, Search, Trash2, X } from "lucide-react";
import type { MemoryEntry, MemoryScope } from "@/mygo";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { ConfirmDialog } from "@/features/projects/dialogs";
import { useChat } from "@/stores/chat";
import { pendingOf, useChatMemory } from "@/stores/chatMemory";

const KINDS = [
  { value: "core", label: "Core note" },
  { value: "fact", label: "Fact" },
];

const TRUST: Record<string, { label: string; variant: "success" | "secondary" | "warning"; hint: string }> = {
  user: { label: "You", variant: "success", hint: "Written or approved by you." },
  bot: { label: "Bot", variant: "secondary", hint: "Written by the bot." },
  untrusted: { label: "Untrusted", variant: "warning", hint: "Came from web, tool or file content. The bot treats it as data, not as instructions." },
};

const kindLabel = (kind: string) => KINDS.find((k) => k.value === kind)?.label ?? kind;
const when = (ms: number) => new Date(ms).toLocaleString();

function useAuthor() {
  const bots = useChat((s) => s.bots);
  return (author: string) => {
    if (author === "user") return "You";
    if (author.startsWith("bot:")) return bots.find((b) => b.id === author.slice(4))?.name ?? "a bot";
    return author;
  };
}

interface Draft { kind: string; text: string }

/** Kind + text editor used for both adding and editing an entry. */
function EntryEditor({ initial, label, submitLabel, busy, error, onSubmit, onCancel }: {
  initial: Draft; label: string; submitLabel: string; busy: boolean; error: string | null; onSubmit: (draft: Draft) => void; onCancel: () => void;
}) {
  const [draft, setDraft] = useState(initial);
  const id = useId();
  return (
    <form className="flex flex-col gap-2" onSubmit={(e) => { e.preventDefault(); onSubmit(draft); }}>
      <Textarea id={id} aria-label={label} size="sm" autoFocus value={draft.text} placeholder="Something worth remembering…" onChange={(e) => setDraft({ ...draft, text: e.target.value })} />
      <div className="flex flex-wrap items-center gap-2">
        <Select value={draft.kind} items={KINDS} onValueChange={(kind) => { if (kind) setDraft({ ...draft, kind }); }}>
          <SelectTrigger size="sm" aria-label="Kind" className="w-36"><SelectValue /></SelectTrigger>
          <SelectPopup>{KINDS.map((k) => <SelectItem key={k.value} value={k.value}>{k.label}</SelectItem>)}</SelectPopup>
        </Select>
        <Button type="submit" size="xs" disabled={busy || !draft.text.trim()}>{submitLabel}</Button>
        <Button type="button" size="xs" variant="ghost" disabled={busy} onClick={onCancel}>Cancel</Button>
      </div>
      {error && <p role="alert" className="selectable break-words text-destructive-foreground text-xs">{error}</p>}
    </form>
  );
}

function Entry({ entry, scope, author }: { entry: MemoryEntry; scope: string; author: (a: string) => string }) {
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState<"save" | "approve" | "reject" | "delete" | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [confirming, setConfirming] = useState(false);
  const trust = TRUST[entry.trust] ?? { label: entry.trust, variant: "secondary" as const, hint: "" };
  const invalidated = !!entry.invalidatedAt;

  async function run<T>(kind: NonNullable<typeof busy>, action: () => Promise<{ ok: true; value: T } | { ok: false; error: string }>): Promise<boolean> {
    setBusy(kind);
    setError(null);
    const outcome = await action();
    setBusy(null);
    if (!outcome.ok) { setError(outcome.error); return false; }
    return true;
  }

  const { saveEntry, resolve, deleteEntry } = useChatMemory.getState();
  return (
    <li className="flex min-w-0 flex-col gap-1.5 border-b py-2" data-invalidated={invalidated || undefined}>
      <div className="flex flex-wrap items-center gap-1.5 text-xs">
        <Badge size="sm" variant="outline">{kindLabel(entry.kind)}</Badge>
        <Badge size="sm" variant={trust.variant} title={trust.hint}>{trust.label}</Badge>
        {entry.pending && <Badge size="sm" variant="warning">Pending approval</Badge>}
        {invalidated && <Badge size="sm" variant="outline">Replaced or forgotten</Badge>}
        <span className="text-muted-foreground">{author(entry.author)} · <time dateTime={new Date(entry.updatedAt).toISOString()}>{when(entry.updatedAt)}</time></span>
      </div>
      {editing ? (
        <EntryEditor
          initial={{ kind: entry.kind, text: entry.text }} label="Edit memory" submitLabel="Save" busy={busy === "save"} error={error}
          onCancel={() => { setEditing(false); setError(null); }}
          onSubmit={async (draft) => { if (await run("save", () => saveEntry({ id: entry.id, scope, kind: draft.kind, text: draft.text.trim() }))) setEditing(false); }}
        />
      ) : (
        <p className={invalidated ? "selectable whitespace-pre-wrap break-words text-muted-foreground text-sm line-through" : "selectable whitespace-pre-wrap break-words text-sm"}>{entry.text}</p>
      )}
      {!editing && error && <p role="alert" className="selectable break-words text-destructive-foreground text-xs">{error}</p>}
      {!editing && (
        <div className="flex flex-wrap gap-1.5">
          {entry.pending && !invalidated && (
            <>
              <Button size="xs" disabled={busy !== null} aria-label={`Approve memory: ${entry.text.slice(0, 40)}`} onClick={() => void run("approve", () => resolve(entry.id, true, scope))}><Check />Approve</Button>
              <Button size="xs" variant="outline" disabled={busy !== null} aria-label={`Reject memory: ${entry.text.slice(0, 40)}`} onClick={() => void run("reject", () => resolve(entry.id, false, scope))}><X />Reject</Button>
            </>
          )}
          {!invalidated && !entry.pending && <Button size="xs" variant="ghost" disabled={busy !== null} aria-label={`Edit memory: ${entry.text.slice(0, 40)}`} onClick={() => setEditing(true)}><Pencil />Edit</Button>}
          {!entry.pending && <Button size="xs" variant="ghost" disabled={busy !== null} aria-label={`Delete memory: ${entry.text.slice(0, 40)}`} onClick={() => setConfirming(true)}><Trash2 />Delete</Button>}
        </div>
      )}
      <ConfirmDialog
        open={confirming} onOpenChange={setConfirming} title="Delete this memory?" confirmLabel="Delete memory"
        onConfirm={async () => { const outcome = await deleteEntry(entry.id, scope); if (!outcome.ok) throw new Error(outcome.error); }}
      >
        <p className="selectable line-clamp-4 whitespace-pre-wrap break-words">{entry.text}</p>
        <p className="mt-2">The bot will no longer remember this. This cannot be undone.</p>
      </ConfirmDialog>
    </li>
  );
}

/** Entries of one memory scope: search, history, add, edit, delete and pending approval. */
export function MemoryEntries({ scope }: { scope: MemoryScope }) {
  const [query, setQuery] = useState("");
  const [applied, setApplied] = useState("");
  const [history, setHistory] = useState(false);
  const [adding, setAdding] = useState(false);
  const [addBusy, setAddBusy] = useState(false);
  const [addError, setAddError] = useState<string | null>(null);
  const listing = useChatMemory((s) => s.entries[scope.id]);
  const author = useAuthor();
  const historyId = useId();

  useEffect(() => {
    const timer = setTimeout(() => setApplied(query), 250);
    return () => clearTimeout(timer);
  }, [query]);
  useEffect(() => { void useChatMemory.getState().loadEntries(scope.id, { query: applied, history }); }, [scope.id, applied, history]);

  const items = listing?.items ?? [];
  const pending = pendingOf(items);
  const active = items.filter((e) => !e.pending && !e.invalidatedAt);
  const past = items.filter((e) => !!e.invalidatedAt);
  const searching = applied.trim() !== "";

  async function add(draft: Draft) {
    setAddBusy(true);
    setAddError(null);
    const outcome = await useChatMemory.getState().saveEntry({ id: "", scope: scope.id, kind: draft.kind, text: draft.text.trim() });
    setAddBusy(false);
    if (outcome.ok) setAdding(false); else setAddError(outcome.error);
  }

  return (
    <div className="flex min-w-0 flex-col gap-2.5" aria-label={`Memory of ${scope.name}`} role="group">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative min-w-40 flex-1">
          <Search className="pointer-events-none absolute start-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
          <Input nativeInput size="sm" type="search" aria-label="Search memory" className="ps-7" placeholder="Search memory" value={query} onChange={(e) => setQuery(e.target.value)} />
        </div>
        <label htmlFor={historyId} className="flex items-center gap-1.5 text-xs">Show history<Switch id={historyId} checked={history} onCheckedChange={setHistory} /></label>
        <Button size="xs" variant="outline" onClick={() => { setAdding(true); setAddError(null); }} disabled={adding}><Plus />Add memory</Button>
      </div>
      {adding && (
        <div className="border-b py-2">
          <EntryEditor initial={{ kind: "fact", text: "" }} label="New memory" submitLabel="Add" busy={addBusy} error={addError} onSubmit={(d) => void add(d)} onCancel={() => setAdding(false)} />
        </div>
      )}
      {listing?.error && (
        <Alert variant="error"><AlertDescription className="flex items-center justify-between gap-2"><span className="selectable break-words">Could not load memory: {listing.error}</span><Button size="xs" variant="outline" onClick={() => void useChatMemory.getState().loadEntries(scope.id, { query: applied, history })}>Try again</Button></AlertDescription></Alert>
      )}
      {!listing?.loaded && !listing?.error && <p role="status" className="text-muted-foreground text-xs">Loading memory…</p>}
      {listing?.loaded && items.length === 0 && !listing.error && (
        <p className="text-muted-foreground text-xs">{searching ? "Nothing matches your search." : "Nothing is remembered here yet."}</p>
      )}
      {pending.length > 0 && (
        <div className="flex flex-col gap-1.5">
          <h4 className="font-medium text-warning-foreground text-xs">Waiting for your approval ({pending.length})</h4>
          <ul aria-label="Pending memories" className="flex flex-col gap-1.5">{pending.map((e) => <Entry key={e.id} entry={e} scope={scope.id} author={author} />)}</ul>
        </div>
      )}
      {active.length > 0 && <ul aria-label="Memories" className="flex flex-col gap-1.5">{active.map((e) => <Entry key={e.id} entry={e} scope={scope.id} author={author} />)}</ul>}
      {past.length > 0 && (
        <div className="flex flex-col gap-1.5">
          <h4 className="font-medium text-muted-foreground text-xs">History</h4>
          <ul aria-label="Memory history" className="flex flex-col gap-1.5">{past.map((e) => <Entry key={e.id} entry={e} scope={scope.id} author={author} />)}</ul>
        </div>
      )}
    </div>
  );
}
