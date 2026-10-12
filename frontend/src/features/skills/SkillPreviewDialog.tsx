import { useEffect, useState } from "react";
import { Check, TriangleAlert } from "lucide-react";
import { Skills, type HubPreview, type Skill } from "@/mygo";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Dialog, DialogDescription, DialogFooter, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { Spinner } from "@/components/ui/spinner";
import { errText } from "@/features/projects/dialogs";

export type HubTarget = { source: string; skillId: string; name: string };

/**
 * Shows exactly what adding a skills.sh skill puts in the registry (the full SKILL.md, its files,
 * and any scripts) before it is added. A skill is instructions an agent follows, so it is read first.
 * Once added it is enabled for every agent through Orole; copying it into Claude Code, Codex or Oh My
 * Pi's own folders is the separate "Update agents" step, offered right here.
 */
export function SkillPreviewDialog({ target, added, canUpdateAgents, onClose, onAdded, onUpdateAgents }: {
  target: HubTarget | null;
  added: boolean;
  canUpdateAgents: boolean;
  onClose: () => void;
  onAdded: (skill: Skill) => void;
  onUpdateAgents: () => Promise<void>;
}) {
  const [preview, setPreview] = useState<HubPreview | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState<Skill | null>(null);
  const [updated, setUpdated] = useState(false);
  const source = target?.source;
  const skillId = target?.skillId;
  useEffect(() => {
    setPreview(null); setError(null); setDone(null); setUpdated(false); setBusy(false);
    if (!source || !skillId) return;
    let live = true;
    Skills.hubPreview(source, skillId).then((p) => { if (live) setPreview(p); }, (e) => { if (live) setError(errText(e)); });
    return () => { live = false; };
  }, [source, skillId]);

  async function add() {
    if (!target) return;
    setBusy(true); setError(null);
    try { const skill = await Skills.hubInstall(target.source, target.skillId); setDone(skill); onAdded(skill); }
    catch (e) { setError(errText(e)); }
    finally { setBusy(false); }
  }
  async function update() {
    setBusy(true); setError(null);
    try { await onUpdateAgents(); setUpdated(true); }
    catch (e) { setError(errText(e)); }
    finally { setBusy(false); }
  }

  const owner = target?.source.split("/")[0] ?? "";
  const isAdded = added || done !== null;
  return <Dialog open={target !== null} onOpenChange={(open) => { if (!open && !busy) onClose(); }}>
    <DialogPopup className="max-w-2xl">
      <DialogHeader>
        <DialogTitle>{target?.name}</DialogTitle>
        <DialogDescription>by {owner} <span className="selectable font-mono text-xs">· {target?.source}</span></DialogDescription>
      </DialogHeader>
      <DialogPanel>
        {!preview && !error && <div role="status" className="flex items-center gap-2 text-muted-foreground"><Spinner className="size-4" />Reading the skill from GitHub…</div>}
        {error && <p role="alert" className="mb-3 break-words text-destructive-foreground text-sm">{error}</p>}
        {done && <Alert variant="success" className="mb-3"><Check /><AlertTitle>Added to your skills</AlertTitle>
          <AlertDescription>
            {updated ? "Copied into your coding agents." : "Agents started from Orole can use it now."}
            {!updated && canUpdateAgents && " To use it when you run Claude Code, Codex or Oh My Pi yourself, update your agents."}
          </AlertDescription></Alert>}
        {preview && <div className="flex flex-col gap-3">
          <p className="text-sm">{preview.description}</p>
          {preview.scripts.length > 0 && <Alert variant="warning"><TriangleAlert /><AlertTitle>Includes scripts</AlertTitle>
            <AlertDescription>Agents may run these when they follow the skill: <span className="selectable font-mono text-xs">{preview.scripts.join(", ")}</span></AlertDescription></Alert>}
          {preview.files.length > 0 && <p className="text-muted-foreground text-xs">{preview.files.length} more {preview.files.length === 1 ? "file" : "files"}: <span className="selectable font-mono">{preview.files.slice(0, 8).join(", ")}{preview.files.length > 8 ? ", …" : ""}</span></p>}
          <div>
            <h3 className="mb-1 font-medium text-xs">SKILL.md</h3>
            <pre tabIndex={0} aria-label="SKILL.md contents" className="selectable max-h-80 overflow-auto whitespace-pre-wrap break-words rounded-lg border bg-muted/40 p-3 font-mono text-xs">{preview.body}</pre>
          </div>
        </div>}
      </DialogPanel>
      <DialogFooter>
        <Button variant="outline" disabled={busy} onClick={onClose}>{isAdded ? "Done" : "Cancel"}</Button>
        {done && !updated && canUpdateAgents && <Button variant="secondary" disabled={busy} onClick={() => void update()}>{busy ? "Updating…" : "Update agents"}</Button>}
        {!isAdded && <Button disabled={busy || !preview} onClick={() => void add()}>{busy ? "Adding…" : "Add skill"}</Button>}
        {isAdded && !done && <Button disabled>Added</Button>}
      </DialogFooter>
    </DialogPopup>
  </Dialog>;
}
