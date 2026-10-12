import { Trash2Icon } from "lucide-react";
import { useEffect, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { errText } from "@/features/projects/dialogs";
import { Chat, type AutomationSignal } from "@/mygo";

/**
 * Requests the bot noticed the user repeating (the same integration check on separate days), which it
 * may offer once to automate. Each can be forgotten; it then starts counting from zero.
 */
export function AutomationIdeas({ botId }: { botId: string }) {
  const [signals, setSignals] = useState<AutomationSignal[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let current = true;
    Chat.automationSignals(botId).then(
      (list) => { if (current) { setSignals(list ?? []); setError(null); } },
      (cause: unknown) => { if (current) setError(errText(cause)); },
    );
    return () => { current = false; };
  }, [botId]);

  async function forget(signal: AutomationSignal) {
    try {
      await Chat.deleteAutomationSignal({ botId, key: signal.key });
      setSignals((list) => (list ?? []).filter((s) => s.key !== signal.key));
      setError(null);
    } catch (cause) {
      setError(errText(cause));
    }
  }

  return <section aria-label="Automation ideas" className="flex flex-col gap-2">
    <h3 className="font-medium text-xs text-muted-foreground">Automation ideas</h3>
    {error && <p role="alert" className="selectable break-words text-destructive-foreground text-xs">{error}</p>}
    {!error && signals === null && <p role="status" className="text-muted-foreground text-xs">Loading…</p>}
    {signals?.length === 0 && <p className="text-muted-foreground text-xs">Nothing yet. When you ask for the same check on different days, the bot may offer to automate it.</p>}
    {!!signals?.length && <ul aria-label="Automation ideas" className="flex flex-col divide-y">
      {signals.map((s) => <li key={s.key} className="flex items-start gap-2 py-2">
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <p className="selectable break-all font-mono text-xs">{s.label}</p>
          <span className="flex flex-wrap items-center gap-2 text-muted-foreground text-xs">
            {s.count === 1 ? "Once" : `${s.count} times`} on {s.days === 1 ? "1 day" : `${s.days} days`}
            {s.ready && <Badge size="sm" variant="info">May suggest</Badge>}
            {s.answered && <Badge size="sm" variant="secondary">Answered</Badge>}
          </span>
        </div>
        <Button type="button" variant="ghost" size="icon-xs" aria-label={`Forget idea: ${s.label}`} onClick={() => void forget(s)}><Trash2Icon aria-hidden /></Button>
      </li>)}
    </ul>}
  </section>;
}
