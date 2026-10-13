import { useCallback, useEffect, useState } from "react";
import { Skills, type Discovered } from "@/mygo";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogDescription, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { Spinner } from "@/components/ui/spinner";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { ProviderLogo } from "@/features/agents/ProviderLogo";
import { errText, toastError } from "@/features/projects/dialogs";
import { groupDiscovered, harnessLabel } from "./skills";

export function ImportDialog({ open, onOpenChange, onImported }: { open: boolean; onOpenChange: (o: boolean) => void; onImported: () => void }) {
  const [found, setFound] = useState<Discovered[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const key = (d: Discovered) => `${d.harness}/${d.name}`;

  const discover = useCallback(async () => {
    setFound(null); setError(null);
    try { setFound(await Skills.discover()); } catch (e) { setError(errText(e)); }
  }, []);
  useEffect(() => { if (open) void discover(); }, [open, discover]);

  async function run(items: Discovered[], id: string) {
    setBusy(id);
    const done = new Set<string>();
    try {
      for (const d of items) { await Skills.import(d.harness, d.name); done.add(key(d)); }
    } catch (e) { toastError("Could not import skill", e); }
    finally {
      setBusy(null);
      if (done.size) { setFound((f) => f?.filter((d) => !done.has(key(d))) ?? f); onImported(); }
    }
  }

  const groups = groupDiscovered(found ?? []);
  return <Dialog open={open} onOpenChange={(o) => { if (!busy) onOpenChange(o); }}>
    <DialogPopup className="max-w-2xl">
      <DialogHeader><DialogTitle>Import from your agents</DialogTitle><DialogDescription>Skills found in your agents&rsquo; own folders that Orole does not hold yet. Importing copies them into Orole.</DialogDescription></DialogHeader>
      <DialogPanel>
        {error && <p role="alert" className="text-destructive-foreground text-sm">{error} <button className="underline" onClick={() => void discover()}>Retry</button></p>}
        {!found && !error && <div role="status" className="flex items-center gap-2 text-muted-foreground"><Spinner className="size-4" />Scanning agent folders…</div>}
        {found && groups.length === 0 && <p className="text-muted-foreground">Nothing to import. Every skill in your agents is already in Orole.</p>}
        <div className="flex flex-col gap-4">
          {groups.map((g) => <section key={g.harness} aria-label={harnessLabel(g.harness)}>
            <div className="mb-1 flex items-center gap-2"><ProviderLogo kind={g.harness} /><h3 className="font-medium text-sm">{harnessLabel(g.harness)}</h3><Badge size="sm" variant="outline">{g.skills.length}</Badge>
              <Button size="xs" variant="outline" className="ml-auto" disabled={busy !== null} onClick={() => void run(g.skills, g.harness)}>{busy === g.harness ? "Importing…" : "Import all"}</Button></div>
            <ul className="flex flex-col divide-y rounded-lg border">
              {g.skills.map((d) => <li key={key(d)} className="flex items-center gap-2 p-2">
                <div className="min-w-0 flex-1"><p className="truncate font-medium">{d.name}</p><p className="line-clamp-2 text-muted-foreground text-xs">{d.description || "No description"}</p><Tooltip><TooltipTrigger render={<p className="selectable truncate font-mono text-muted-foreground text-xs" />}>{d.path}</TooltipTrigger><TooltipPopup className="max-w-sm break-all">{d.path}</TooltipPopup></Tooltip></div>
                <Button size="xs" variant="outline" disabled={busy !== null} aria-label={`Import ${d.name} from ${harnessLabel(d.harness)}`} onClick={() => void run([d], key(d))}>{busy === key(d) ? "Importing…" : "Import"}</Button>
              </li>)}
            </ul>
          </section>)}
        </div>
      </DialogPanel>
    </DialogPopup>
  </Dialog>;
}
