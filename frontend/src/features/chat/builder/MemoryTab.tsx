import { useEffect, useId, useState, type Dispatch } from "react";
import { Brain, Plus, Trash2 } from "lucide-react";
import type { MemoryScope } from "@/mygo";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Meter, MeterIndicator, MeterTrack } from "@/components/ui/meter";
import { Switch } from "@/components/ui/switch";
import { ConfirmDialog } from "@/features/projects/dialogs";
import { cn } from "@/lib/utils";
import { useChatMemory } from "@/stores/chatMemory";
import type { Action, FormState } from "./botForm";
import { Section } from "./controls";
import { MemoryEntries } from "./MemoryEntries";

const KIND_LABEL: Record<string, string> = { bot: "Private", user: "About you", shared: "Shared" };

function Usage({ scope }: { scope: MemoryScope }) {
  const full = scope.limit > 0 && scope.used / scope.limit >= 0.9;
  return (
    <Meter value={Math.min(scope.used, Math.max(scope.limit, 1))} min={0} max={Math.max(scope.limit, 1)} aria-label={`Core memory used in ${scope.name}`} className="gap-1">
      <MeterTrack className="h-1.5 rounded-full"><MeterIndicator className={cn("rounded-full", full && "bg-warning")} /></MeterTrack>
      <span className="text-muted-foreground text-xs tabular-nums">{scope.count} {scope.count === 1 ? "entry" : "entries"}</span>
    </Meter>
  );
}

function ScopeCard({ scope, selected, form, dispatch, onSelect, onDelete }: {
  scope: MemoryScope; selected: boolean; form: FormState; dispatch: Dispatch<Action>; onSelect: () => void; onDelete: () => void;
}) {
  const grant = form.memory.find((a) => a.scope === scope.id);
  const private_ = scope.kind === "bot";
  const readId = useId();
  const writeId = useId();
  return (
    <li className={cn("flex min-w-0 flex-col gap-2 border-b py-2", selected && "border-l-2 border-l-ring pl-2")}>
      <div className="flex items-start justify-between gap-2">
        <button type="button" aria-pressed={selected} onClick={onSelect} className="flex min-w-0 flex-1 flex-col items-start gap-0.5 rounded text-start outline-none focus-visible:ring-2 focus-visible:ring-ring">
          <span className="selectable break-words font-medium text-sm">{scope.name}</span>
          <span className="text-muted-foreground text-xs">{KIND_LABEL[scope.kind] ?? scope.kind}</span>
        </button>
        {scope.kind === "shared" && <Button size="icon-sm" variant="ghost" aria-label={`Delete shared scope ${scope.name}`} onClick={onDelete}><Trash2 /></Button>}
      </div>
      <Usage scope={scope} />
      {!private_ && (
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs">
          <span className="flex items-center gap-1.5"><span id={readId}>{"Read "}<span className="sr-only">{scope.name}</span></span><Switch aria-labelledby={readId} checked={grant?.read ?? false} onCheckedChange={(read) => dispatch({ type: "access", scope: scope.id, read, write: read && (grant?.write ?? false) })} /></span>
          <span className="flex items-center gap-1.5"><span id={writeId}>{"Write "}<span className="sr-only">{scope.name}</span></span><Switch aria-labelledby={writeId} checked={grant?.write ?? false} onCheckedChange={(write) => dispatch({ type: "access", scope: scope.id, read: grant?.read ?? false, write })} /></span>
        </div>
      )}
    </li>
  );
}

/** Memory of an existing bot: scopes with usage and access, and the entries of the selected scope. */
export function MemoryTab({ botId, form, dispatch }: { botId: string | null; form: FormState; dispatch: Dispatch<Action> }) {
  const listing = useChatMemory((s) => (botId ? s.scopes[botId] : undefined));
  const [picked, setPicked] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<MemoryScope | null>(null);

  useEffect(() => {
    if (!botId) return;
    const off = useChatMemory.getState().startSync();
    void useChatMemory.getState().loadScopes(botId);
    return off;
  }, [botId]);

  if (!botId) {
    return (
      <Section title="Memory">
        <p role="status" className="flex items-center gap-2 text-muted-foreground text-sm"><Brain className="size-4" aria-hidden="true" />Save the bot first. Then you can see, edit and approve what it remembers here.</p>
      </Section>
    );
  }

  const scopes = listing?.items ?? [];
  const selected = scopes.find((s) => s.id === picked) ?? scopes[0];

  async function create() {
    const trimmed = name.trim();
    if (!trimmed) return;
    setCreating(true);
    setCreateError(null);
    const outcome = await useChatMemory.getState().createScope(trimmed, botId!);
    setCreating(false);
    if (outcome.ok) { setName(""); setPicked(outcome.value.id); } else setCreateError(outcome.error);
  }

  return (
    <div className="flex flex-col gap-3">
      <Section title="Memory">
        {listing?.error && (
          <Alert variant="error"><AlertDescription className="flex items-center justify-between gap-2"><span className="selectable break-words">Could not load memory: {listing.error}</span><Button size="xs" variant="outline" onClick={() => void useChatMemory.getState().loadScopes(botId)}>Try again</Button></AlertDescription></Alert>
        )}
        {!listing?.loaded && !listing?.error && <p role="status" className="text-muted-foreground text-xs">Loading memory…</p>}
        {scopes.length > 0 && (
          <ul aria-label="Memory scopes" className="flex flex-col gap-2">
            {scopes.map((scope) => (
              <ScopeCard key={scope.id} scope={scope} selected={scope.id === selected?.id} form={form} dispatch={dispatch} onSelect={() => setPicked(scope.id)} onDelete={() => setDeleting(scope)} />
            ))}
          </ul>
        )}
        <form className="flex flex-col gap-1.5" onSubmit={(e) => { e.preventDefault(); void create(); }}>
          <div className="flex gap-1.5">
            <Input nativeInput size="sm" aria-label="New shared scope name" placeholder="New shared scope" value={name} onChange={(e) => { setName(e.target.value); setCreateError(null); }} />
            <Button type="submit" size="sm" variant="outline" disabled={creating || !name.trim()}><Plus />Create</Button>
          </div>
          {createError && <p role="alert" className="selectable break-words text-destructive-foreground text-xs">{createError}</p>}
        </form>
      </Section>

      {selected && (
        <Section title="Entries">
          <MemoryEntries key={selected.id} scope={selected} />
        </Section>
      )}

      <ConfirmDialog
        open={deleting !== null} onOpenChange={(o) => { if (!o) setDeleting(null); }}
        title={`Delete the shared scope “${deleting?.name ?? ""}”?`} confirmLabel="Delete scope"
        onConfirm={async () => {
          if (!deleting) return;
          const outcome = await useChatMemory.getState().deleteScope(deleting.id, botId);
          if (!outcome.ok) throw new Error(outcome.error);
          dispatch({ type: "access", scope: deleting.id, read: false, write: false });
          setPicked(null);
        }}
      >
        <p>All {deleting?.count ?? 0} entries in it are deleted for every bot that uses it. This cannot be undone.</p>
      </ConfirmDialog>
    </div>
  );
}
