import { CheckIcon, PencilIcon, PlusIcon, Trash2Icon, XIcon } from "lucide-react";
import { useEffect, useId, useState, type KeyboardEvent } from "react";
import { Button } from "@/components/ui/button";
import { Kbd } from "@/components/ui/kbd";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { ConfirmDialog } from "@/features/projects/dialogs";
import type { MemoryEntry, MemoryScope } from "@/mygo";
import { useChatMemory } from "@/stores/chatMemory";
import { Group, PageHeader } from "./rows";

/** The daemon's cap on one entry's text (internal/chat/memory maxEntryChars). */
const MAX_ENTRY_CHARS = 4000;

/** A bot's own memory scope: the one every bot always reads and writes. */
export const ownScope = (scopes: readonly MemoryScope[] | undefined): MemoryScope | undefined => scopes?.find((s) => s.kind === "bot");

/** Keeps a bot's memory scopes loaded and in step with the daemon while a settings page needs them. */
export function useBotMemory(botId: string) {
  useEffect(() => useChatMemory.getState().startSync(), []);
  useEffect(() => { void useChatMemory.getState().loadScopes(botId); }, [botId]);
  return useChatMemory((s) => s.scopes[botId]);
}

type Outcome = { ok: boolean; error?: string };

/**
 * A box for writing one memory: the text, Cancel and a save button. Ctrl+Enter saves, Escape cancels.
 * The text stays when saving fails, with the reason underneath.
 */
function MemoryEditor({ initial = "", label, submit, onSubmit, onCancel }: {
  initial?: string; label: string; submit: string; onSubmit: (text: string) => Promise<Outcome>; onCancel: () => void;
}) {
  const [text, setText] = useState(initial);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const errorId = useId();
  const ready = text.trim() !== "" && text.trim() !== initial.trim() && !busy;

  const save = async () => {
    if (!ready) return;
    setBusy(true);
    const outcome = await onSubmit(text.trim());
    setBusy(false);
    if (!outcome.ok) setError(outcome.error ?? "That did not work.");
  };
  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) { event.preventDefault(); void save(); }
    else if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); onCancel(); }
  };

  return <div className="flex flex-col gap-1.5 rounded-xl bg-muted/60 p-1.5">
    <Textarea
      unstyled
      autoFocus
      aria-label={label}
      aria-describedby={error ? errorId : undefined}
      aria-invalid={!!error || undefined}
      placeholder="Something the bot should remember"
      maxLength={MAX_ENTRY_CHARS}
      value={text}
      className="text-sm [&_textarea]:min-h-14 [&_textarea]:px-1.5 [&_textarea]:py-1"
      onChange={(event) => { setText(event.target.value); setError(null); }}
      onKeyDown={onKeyDown}
      onFocus={(event) => { const end = event.currentTarget.value.length; event.currentTarget.setSelectionRange(end, end); }}
    />
    {error && <p id={errorId} role="alert" className="selectable break-words px-1.5 text-destructive-foreground text-xs">{error}</p>}
    <div className="flex justify-end gap-1.5">
      <Button type="button" variant="ghost" size="xs" onClick={onCancel}>Cancel</Button>
      <Button type="button" size="xs" disabled={!ready} loading={busy} onClick={() => void save()}>
        {submit}<Kbd aria-hidden className="h-4 bg-primary-foreground/16 text-primary-foreground">⌃↵</Kbd>
      </Button>
    </div>
  </div>;
}

/** One saved memory: its text, and on hover or focus Edit and Forget. A proposed one shows Approve and Reject instead. */
function MemoryRow({ entry, editing, onEdit, onDone, act }: {
  entry: MemoryEntry; editing: boolean; onEdit: () => void; onDone: () => void; act: (run: () => Promise<Outcome>) => Promise<void>;
}) {
  const store = useChatMemory.getState;
  if (editing) {
    return <div className="p-1.5">
      <MemoryEditor
        initial={entry.text}
        label={`Edit memory: ${entry.text}`}
        submit="Save"
        onCancel={onDone}
        onSubmit={async (text) => {
          const outcome = await store().saveEntry({ id: entry.id, scope: entry.scope, kind: entry.kind, text });
          if (outcome.ok) onDone();
          return outcome;
        }}
      />
    </div>;
  }
  return <div className="group/memory flex min-h-10 items-start gap-2 px-3 py-2">
    <p className="selectable min-w-0 flex-1 break-words text-sm">{entry.text}</p>
    <div className={entry.pending ? "flex shrink-0 gap-0.5" : "-my-0.5 flex shrink-0 gap-0.5 opacity-0 transition-opacity focus-within:opacity-100 group-hover/memory:opacity-100 pointer-coarse:opacity-100"}>
      {entry.pending ? <>
        <Button type="button" variant="ghost" size="icon-xs" aria-label={`Approve memory: ${entry.text}`} onClick={() => void act(() => store().resolve(entry.id, true, entry.scope))}><CheckIcon aria-hidden /></Button>
        <Button type="button" variant="ghost" size="icon-xs" aria-label={`Reject memory: ${entry.text}`} onClick={() => void act(() => store().resolve(entry.id, false, entry.scope))}><XIcon aria-hidden /></Button>
      </> : <>
        <Button type="button" variant="ghost" size="icon-xs" aria-label={`Edit: ${entry.text}`} onClick={onEdit}><PencilIcon aria-hidden /></Button>
        <Button type="button" variant="ghost" size="icon-xs" aria-label={`Forget: ${entry.text}`} onClick={() => void act(() => store().deleteEntry(entry.id, entry.scope))}><Trash2Icon aria-hidden /></Button>
      </>}
    </div>
  </div>;
}

/**
 * What the bot remembers about its own work, grouped the way the bot uses it: notes it always has in mind (with their
 * character budget) and facts it recalls when they are relevant. Entries the bot only proposed wait for approval at the
 * top. The user can add a note, edit or forget any entry, or forget everything.
 */
export function MemoriesPage({ botId, onBack }: { botId: string; onBack: () => void }) {
  const scopes = useBotMemory(botId);
  const scope = ownScope(scopes?.items);
  const scopeId = scope?.id ?? "";
  const listing = useChatMemory((s) => s.entries[scopeId]);
  const [error, setError] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  const [forgetting, setForgetting] = useState(false);
  useEffect(() => {
    if (scopeId) void useChatMemory.getState().loadEntries(scopeId, { query: "", history: false });
  }, [scopeId]);

  const act = async (run: () => Promise<Outcome>) => {
    const outcome = await run();
    setError(outcome.ok ? null : (outcome.error ?? "That did not work."));
  };
  const items = listing?.items ?? [];
  const pending = items.filter((e) => e.pending);
  const core = items.filter((e) => !e.pending && e.kind === "core");
  const facts = items.filter((e) => !e.pending && e.kind !== "core");
  const loading = !scopes?.loaded || (scopeId !== "" && !listing?.loaded);
  const failure = scopes?.error ?? listing?.error ?? error;
  const rows = (entries: MemoryEntry[]) => entries.map((entry) => <MemoryRow key={entry.id} entry={entry} editing={editing === entry.id} onEdit={() => { setEditing(entry.id); setAdding(false); }} onDone={() => setEditing(null)} act={act} />);
  const forgetAll = async () => {
    const store = useChatMemory.getState();
    for (const entry of items) {
      const outcome = entry.pending ? await store.resolve(entry.id, false, entry.scope) : await store.deleteEntry(entry.id, entry.scope);
      if (!outcome.ok) throw new Error(outcome.error ?? "That did not work.");
    }
  };

  const addButton = <Tooltip>
    <TooltipTrigger render={<Button type="button" variant="ghost" size="icon-xs" aria-label="Add memory" disabled={!scopeId || adding} onClick={() => { setAdding(true); setEditing(null); }} />}><PlusIcon aria-hidden /></TooltipTrigger>
    <TooltipPopup>Add memory</TooltipPopup>
  </Tooltip>;

  return <div className="flex flex-col gap-4">
    <PageHeader title="Memories" onBack={onBack} action={addButton} />
    {adding && scopeId && <MemoryEditor
      label="New memory"
      submit="Add"
      onCancel={() => setAdding(false)}
      onSubmit={async (text) => {
        const outcome = await useChatMemory.getState().saveEntry({ id: "", scope: scopeId, kind: "core", text });
        if (outcome.ok) setAdding(false);
        return outcome;
      }}
    />}
    {failure && <p role="alert" className="selectable break-words text-destructive-foreground text-xs">{failure}</p>}
    {loading && !failure && <p role="status" className="text-muted-foreground text-xs">Loading…</p>}
    {!loading && !failure && items.length === 0 && !adding && <p className="px-1 text-muted-foreground text-xs">Nothing saved yet. The bot saves what is worth remembering as you talk, or add something with +.</p>}
    {pending.length > 0 && <Group title="Waiting for approval">{rows(pending)}</Group>}
    {core.length > 0 && <Group title="Always in mind" aside={scope && scope.limit > 0 ? `${scope.used.toLocaleString()} / ${scope.limit.toLocaleString()} characters` : undefined}>{rows(core)}</Group>}
    {facts.length > 0 && <Group title="Recalled when relevant">{rows(facts)}</Group>}
    {items.length > 0 && <div className="flex justify-end">
      <Button type="button" variant="ghost" size="xs" className="text-muted-foreground" onClick={() => setForgetting(true)}>Forget all</Button>
    </div>}
    <ConfirmDialog open={forgetting} onOpenChange={setForgetting} title="Forget everything?" confirmLabel="Forget all" onConfirm={forgetAll}>
      The bot loses all {items.length} {items.length === 1 ? "memory" : "memories"} it has saved, and the ones waiting for approval are rejected. This cannot be undone.
    </ConfirmDialog>
  </div>;
}
