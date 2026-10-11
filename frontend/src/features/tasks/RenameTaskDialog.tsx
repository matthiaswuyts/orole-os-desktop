import { useState, type FormEvent } from "react";
import { Agents, type Session } from "@/mygo";
import { useAgents } from "@/stores/agents";
import { Button } from "@/components/ui/button";
import { Dialog, DialogFooter, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { errText } from "@/features/projects/dialogs";

/** Gives a task its own title. The daemon keeps it: automatic naming never replaces a name chosen here. */
export function RenameTaskDialog({ session, open, onOpenChange }: { session: Session; open: boolean; onOpenChange: (open: boolean) => void }) {
  const [title, setTitle] = useState(session.title);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (busy || !title.trim()) return;
    setBusy(true);
    setError(null);
    try {
      useAgents.getState().upsert(await Agents.rename(session.id, title));
      onOpenChange(false);
    } catch (failure) {
      setError(errText(failure));
    } finally {
      setBusy(false);
    }
  };

  return <Dialog open={open} onOpenChange={(next) => { if (!busy) { onOpenChange(next); if (next) { setTitle(session.title); setError(null); } } }}>
    <DialogPopup>
      <form onSubmit={(event) => void submit(event)} className="contents">
        <DialogHeader><DialogTitle>Rename task</DialogTitle></DialogHeader>
        <DialogPanel className="flex flex-col gap-2">
          <Field>
            <FieldLabel>Title</FieldLabel>
            <Input autoFocus value={title} maxLength={120} onChange={(event) => setTitle(event.target.value)} onFocus={(event) => event.currentTarget.select()} />
          </Field>
          {error && <p role="alert" className="text-destructive-foreground text-sm">{error}</p>}
        </DialogPanel>
        <DialogFooter>
          <Button type="button" variant="outline" disabled={busy} onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button type="submit" loading={busy} disabled={!title.trim()}>Rename</Button>
        </DialogFooter>
      </form>
    </DialogPopup>
  </Dialog>;
}
