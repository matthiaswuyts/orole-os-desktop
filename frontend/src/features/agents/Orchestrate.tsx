import { useState } from "react";
import { Ellipsis, Square } from "lucide-react";
import { Agents, type Session } from "@/mygo";
import { useAgents } from "@/stores/agents";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Field, FieldLabel } from "@/components/ui/field";
import { Menu, MenuTrigger, MenuPopup, MenuGroup, MenuItem, MenuSeparator, MenuSub, MenuSubTrigger, MenuSubPopup } from "@/components/ui/menu";
import { Dialog, DialogPopup, DialogHeader, DialogTitle, DialogDescription, DialogPanel, DialogFooter } from "@/components/ui/dialog";
import { Select, SelectTrigger, SelectValue, SelectPopup, SelectGroup, SelectItem } from "@/components/ui/select";
import { isRemoteProcess, newKey, sessionTitle, useAction } from "./format";
import { ErrorNote } from "./shared";

type Operation = "hand-off" | "review-with" | "loop-until" | "try-n";
const titles: Record<Operation, string> = { "hand-off": "Hand off to", "review-with": "Review with", "loop-until": "Loop until", "try-n": "Try N ways" };
export function Orchestrate({ session, onReply }: { session: Session; onReply: () => void }) {
  // Slice selectors: a card must not re-render on every session update of the store.
  const detections = useAgents((s) => s.detections), detecting = useAgents((s) => s.detecting), detectError = useAgents((s) => s.detectError);
  const detect = useAgents((s) => s.detect), upsert = useAgents((s) => s.upsert);
  const [dialog, setDialog] = useState<{ operation: Operation; kind: string } | null>(null);
  const action = useAction();
  const installed = detections?.filter((d) => d.installed) ?? [];
  if (isRemoteProcess(session)) return null;
  return <><Menu onOpenChange={(o) => { if (o && !detections) void detect(false); }}><MenuTrigger render={<Button size="icon-xs" variant="ghost" aria-label={`Orchestrate ${sessionTitle(session)}`} />}><Ellipsis /></MenuTrigger><MenuPopup align="end"><MenuGroup><MenuItem onClick={onReply}>Send prompt</MenuItem>{(["hand-off", "review-with"] as const).map((operation) => <MenuSub key={operation}><MenuSubTrigger>{titles[operation]}</MenuSubTrigger><MenuSubPopup><MenuGroup>{installed.map((d) => <MenuItem key={d.kind} onClick={() => setDialog({ operation, kind: d.kind })}>{d.name}</MenuItem>)}{!installed.length && <MenuItem disabled>{detecting ? "Detecting agents…" : detectError || "No installed agents"}</MenuItem>}</MenuGroup></MenuSubPopup></MenuSub>)}<MenuItem onClick={() => setDialog({ operation: "loop-until", kind: session.kind })}>Loop until…</MenuItem><MenuItem onClick={() => setDialog({ operation: "try-n", kind: installed[0]?.kind ?? "" })}>Try N ways…</MenuItem>{session.live && <><MenuSeparator /><MenuItem disabled={action.busy} onClick={() => void action.run(async () => upsert(await Agents.stop(session.id)))}><Square />Stop agent</MenuItem></>}</MenuGroup></MenuPopup></Menu><ErrorNote error={action.error} />{dialog && <OrchestrationDialog session={session} {...dialog} onClose={() => setDialog(null)} />}</>;
}
function OrchestrationDialog({ session, operation, kind: initialKind, onClose }: { session: Session; operation: Operation; kind: string; onClose: () => void }) {
  const { detections, upsert, refresh } = useAgents();
  const [kind, setKind] = useState(initialKind);
  const [text, setText] = useState("");
  const [command, setCommand] = useState("");
  const [n, setN] = useState(operation === "loop-until" ? 5 : 3);
  const [dir, setDir] = useState(session.project || session.dir);
  const action = useAction();
  const installed = detections?.filter((d) => d.installed) ?? [];
  const needsKind = operation !== "loop-until";
  const selected = installed.find((d) => d.kind === kind);
  return <Dialog open onOpenChange={(o) => { if (!o && !action.busy) onClose(); }}><DialogPopup showCloseButton={!action.busy}><form className="contents" onSubmit={(e) => { e.preventDefault(); void action.run(async () => {
    const key = newKey(); const mode = selected?.modes[0] ?? "terminal";
    if (operation === "hand-off") upsert(await Agents.handOff({ from: session.id, kind, note: text, model: "", mode, permissionMode: "" }, key));
    else if (operation === "review-with") upsert(await Agents.reviewWith({ target: session.id, kind, focus: text, model: "", mode }, key));
    else if (operation === "loop-until") await Agents.loopUntil({ session: session.id, command, maxRounds: n, prompt: text }, key);
    else { const result = await Agents.tryN({ dir, kind, prompt: text, n, model: "", mode, permissionMode: "", baseRef: "" }, key); result.sessions?.forEach(upsert); if (result.errors?.length) { await refresh(); throw new Error(result.errors.join("\n")); } }
    await refresh(); onClose();
  }); }}><DialogHeader><DialogTitle>{titles[operation]}</DialogTitle><DialogDescription>{sessionTitle(session)}</DialogDescription></DialogHeader><DialogPanel className="flex flex-col gap-3">
    {needsKind && <Field><FieldLabel>Agent</FieldLabel><Select value={kind} onValueChange={(v) => v && setKind(v)}><SelectTrigger><SelectValue /></SelectTrigger><SelectPopup><SelectGroup>{installed.map((d) => <SelectItem key={d.kind} value={d.kind}>{d.name}</SelectItem>)}</SelectGroup></SelectPopup></Select></Field>}
    {operation === "loop-until" && <Field><FieldLabel>Command</FieldLabel><Input required value={command} onChange={(e) => setCommand(e.target.value)} placeholder="bun run test" /></Field>}
    {(operation === "loop-until" || operation === "try-n") && <Field><FieldLabel>{operation === "loop-until" ? "Maximum rounds (1–20)" : "Attempts (2–8)"}</FieldLabel><Input type="number" required min={operation === "loop-until" ? 1 : 2} max={operation === "loop-until" ? 20 : 8} value={n} onChange={(e) => setN(Number(e.target.value))} /></Field>}
    {operation === "try-n" && <Field><FieldLabel>Project directory</FieldLabel><Input required value={dir} onChange={(e) => setDir(e.target.value)} /></Field>}
    <Field><FieldLabel>{operation === "try-n" ? "Prompt" : operation === "hand-off" ? "Note (optional)" : operation === "review-with" ? "Focus (optional)" : "Guidance (optional)"}</FieldLabel><Textarea required={operation === "try-n"} value={text} onChange={(e) => setText(e.target.value)} rows={4} /></Field><ErrorNote error={action.error} />
  </DialogPanel><DialogFooter><Button type="button" variant="outline" disabled={action.busy} onClick={onClose}>Cancel</Button><Button type="submit" disabled={action.busy || (needsKind && !selected)}>{action.busy ? "Dispatching…" : titles[operation]}</Button></DialogFooter></form></DialogPopup></Dialog>;
}
