import { useCallback, useEffect, useState } from "react";
import { Columns2, Plus, X } from "lucide-react";
import { Terminals, events, type Info, type Shell } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { Button } from "@/components/ui/button";
import { Menu, MenuItem, MenuPopup, MenuTrigger } from "@/components/ui/menu";
import { cn } from "@/lib/utils";
import { TerminalPane } from "./TerminalPane";
import { NeedsDesktop } from "@/features/shared/States";
import { useSettings } from "@/stores/settings";

export function TerminalsView() {
  const [list, setList] = useState<Info[]>([]);
  const [shells, setShells] = useState<Shell[]>([]);
  const [panes, setPanes] = useState<string[]>([]);
  const [focus, setFocus] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const fontSize = useSettings((s) => s.terminal.fontSize);

  const create = useCallback(async (shell?: string, split = false) => {
    try {
      const info = await Terminals.create(shell ?? "", "", 120, 30);
      setList((l) => [...l.filter((x) => x.id !== info.id), info]);
      setPanes((p) => (split && p.length > 0 && p.length < 3 ? [...p, info.id] : p.length === 0 ? [info.id] : p.map((id, i) => (i === focus ? info.id : id))));
      if (split) setFocus((f) => Math.min(f + 1, 2));
      setError(null);
    } catch (e) { setError(String(e)); }
  }, [focus]);

  useEffect(() => {
    if (!inDesktop) return;
    let live = true;
    void Terminals.shells().then((s) => live && setShells(s)).catch((e) => { if (live) setError(String(e)); });
    void Terminals.list().then((l) => {
      if (!live) return;
      setList(l);
      const running = l.filter((t) => t.state === "running");
      if (running.length) setPanes([running[0]!.id]); else void create(undefined, false);
    }).catch((e) => setError(String(e)));
    const o1 = events.terminalChanged.on((i) => setList((l) => [...l.filter((x) => x.id !== i.id), i]));
    const o2 = events.terminalRemoved.on(({ id }) => { setList((l) => l.filter((x) => x.id !== id)); setPanes((p) => p.filter((x) => x !== id)); });
    return () => { live = false; o1(); o2(); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  if (!inDesktop) return <NeedsDesktop what="Terminals" />;
  const close = (id: string) => void Terminals.close(id).catch((e) => setError(String(e)));
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex items-center gap-1 border-b bg-sidebar px-2 py-1" role="tablist" aria-label="Terminals">
        {list.sort((a, b) => a.startedAt.localeCompare(b.startedAt)).map((t) => (
          <div key={t.id} role="presentation" className={cn("group flex items-center rounded-md", panes.includes(t.id) && "bg-accent")}>
            <button role="tab" aria-selected={panes[focus] === t.id} onClick={() => setPanes((p) => p.length ? p.map((x, i) => (i === focus ? t.id : x)) : [t.id])} className="flex items-center gap-1.5 px-2 py-1">
              <span className={t.state === "running" ? "text-success" : "text-muted-foreground"}>●</span>{t.title}
              {t.state === "exited" && <span className="font-mono text-[10px] text-muted-foreground">exit {t.exitCode}</span>}
            </button>
            <button aria-label={`Close ${t.title}`} className="pr-1.5 opacity-0 group-hover:opacity-100" onClick={() => close(t.id)}><X className="size-3" /></button>
          </div>
        ))}
        <Menu>
          <MenuTrigger render={<Button variant="ghost" size="icon" aria-label="New terminal" />}><Plus /></MenuTrigger>
          <MenuPopup align="start">
            {shells.map((s) => <MenuItem key={s.id} onClick={() => void create(s.id)}>{s.name}</MenuItem>)}
          </MenuPopup>
        </Menu>
        <Button variant="ghost" size="icon" aria-label="Split pane" disabled={panes.length >= 3 || panes.length === 0} onClick={() => void create(undefined, true)}><Columns2 /></Button>
        <span className="flex-1" />
        {error && <span role="alert" className="px-2 text-destructive-foreground">{error}</span>}
      </div>
      <div className="flex min-h-0 flex-1 gap-2 p-2">
        {panes.map((id, i) => <TerminalPane key={id} id={id} active={i === focus} onFocus={() => setFocus(i)} fontSize={fontSize} />)}
      </div>
      <p className="border-t px-3 py-1 text-xs text-muted-foreground">Sessions keep running when this window closes. They end if the daemon stops or Windows restarts.</p>
    </div>
  );
}
