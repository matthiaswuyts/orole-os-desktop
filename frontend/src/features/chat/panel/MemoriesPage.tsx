import { CheckIcon, Trash2Icon, XIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { MemoryScope } from "@/mygo";
import { useChatMemory } from "@/stores/chatMemory";

/** A bot's own memory scope: the one every bot always reads and writes. */
export const ownScope = (scopes: readonly MemoryScope[] | undefined): MemoryScope | undefined => scopes?.find((s) => s.kind === "bot");

/** Keeps a bot's memory scopes loaded and in step with the daemon while a settings page needs them. */
export function useBotMemory(botId: string) {
  useEffect(() => useChatMemory.getState().startSync(), []);
  useEffect(() => { void useChatMemory.getState().loadScopes(botId); }, [botId]);
  return useChatMemory((s) => s.scopes[botId]);
}

/** What the bot remembers about its own work: each entry can be removed; entries the bot only proposed wait for approval. */
export function MemoriesPage({ botId }: { botId: string }) {
  const scopes = useBotMemory(botId);
  const scope = ownScope(scopes?.items);
  const scopeId = scope?.id ?? "";
  const listing = useChatMemory((s) => s.entries[scopeId]);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (scopeId) void useChatMemory.getState().loadEntries(scopeId, { query: "", history: false });
  }, [scopeId]);

  const act = async (run: () => Promise<{ ok: boolean; error?: string }>) => {
    const outcome = await run();
    setError(outcome.ok ? null : (outcome.error ?? "That did not work."));
  };
  const items = listing?.items ?? [];
  const loading = !scopes?.loaded || (scopeId !== "" && !listing?.loaded);
  const failure = scopes?.error ?? listing?.error ?? error;
  return <div className="flex flex-col gap-2">
    {failure && <p role="alert" className="selectable break-words text-destructive-foreground text-xs">{failure}</p>}
    {loading && !failure && <p role="status" className="text-muted-foreground text-xs">Loading…</p>}
    {!loading && !failure && items.length === 0 && <p className="text-muted-foreground text-xs">Nothing saved yet. The bot saves what is worth remembering as you talk.</p>}
    {items.length > 0 && <ul aria-label="Memories" className="flex flex-col divide-y">
      {items.map((entry) => <li key={entry.id} className="flex items-start gap-2 py-2">
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <p className="selectable break-words text-sm">{entry.text}</p>
          {entry.pending && <Badge size="sm" variant="warning" className="self-start">Waiting for approval</Badge>}
        </div>
        {entry.pending && <>
          <Button type="button" variant="ghost" size="icon-xs" aria-label={`Approve memory: ${entry.text}`} onClick={() => void act(() => useChatMemory.getState().resolve(entry.id, true, entry.scope))}><CheckIcon aria-hidden /></Button>
          <Button type="button" variant="ghost" size="icon-xs" aria-label={`Reject memory: ${entry.text}`} onClick={() => void act(() => useChatMemory.getState().resolve(entry.id, false, entry.scope))}><XIcon aria-hidden /></Button>
        </>}
        {!entry.pending && <Button type="button" variant="ghost" size="icon-xs" aria-label={`Forget: ${entry.text}`} onClick={() => void act(() => useChatMemory.getState().deleteEntry(entry.id, entry.scope))}><Trash2Icon aria-hidden /></Button>}
      </li>)}
    </ul>}
  </div>;
}
