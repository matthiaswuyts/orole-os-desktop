import { useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { SearchAddon } from "@xterm/addon-search";
import { WebLinksAddon } from "@xterm/addon-web-links";
import { Unicode11Addon } from "@xterm/addon-unicode11";
import { WebglAddon } from "@xterm/addon-webgl";
import { Search, X } from "lucide-react";
import { Terminals, events } from "@/mygo";
import { cn } from "@/lib/utils";
import { useSettings } from "@/stores/settings";
import { getBuiltinTheme } from "@/themes/builtin";

/** Base64 to bytes with a plain loop: `Uint8Array.from` with a mapper makes a closure call per byte. */
function b64(s: string): Uint8Array {
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

function cssVar(n: string) {
  return getComputedStyle(document.documentElement).getPropertyValue(n).trim();
}

function terminalTheme() {
  return {
    ...getBuiltinTheme(useSettings.getState().appearance.theme).terminal,
    background: "#00000000",
    foreground: cssVar("--foreground") || "#ddd",
    cursor: cssVar("--primary") || "#9bf",
    cursorAccent: cssVar("--background"),
  };
}

/** One xterm.js view bound to a daemon terminal. Reconnect = Attach (scrollback) + live chunks past `seq`. */
export function TerminalPane({ id, active, onFocus, fontSize }: { id: string; active: boolean; onFocus: () => void; fontSize: number }) {
  const host = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const search = useRef<SearchAddon | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const sizeRef = useRef(fontSize); // the size a terminal created for another `id` starts with
  const [finding, setFinding] = useState(false);
  const [query, setQuery] = useState("");
  const [exited, setExited] = useState<number | null>(null);

  useEffect(() => {
    const term = new Terminal({
      fontFamily: '"JetBrains Mono Variable", Consolas, monospace', fontSize: sizeRef.current, cursorBlink: true, scrollback: 10000,
      allowProposedApi: true, theme: terminalTheme(),
      allowTransparency: true,
    });
    const fit = new FitAddon();
    const srch = new SearchAddon();
    term.loadAddon(fit); term.loadAddon(srch); term.loadAddon(new WebLinksAddon()); term.loadAddon(new Unicode11Addon());
    term.unicode.activeVersion = "11";
    termRef.current = term; search.current = srch;
    term.open(host.current!);
    // GPU rendering keeps fast output (builds, logs, an agent's commands) smooth. Without WebGL2, or after the browser
    // drops the context, xterm's own renderer takes over.
    try {
      const gl = new WebglAddon();
      gl.onContextLoss(() => gl.dispose());
      term.loadAddon(gl);
    } catch { /* no WebGL2 here */ }
    fitRef.current = fit;
    fit.fit();
    // Observe the applied CSS rather than React effect order; update live sessions in place.
    const themeObserver = new MutationObserver(() => { term.options.theme = terminalTheme(); });
    themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ["class", "style"] });

    let seq = 0;
    let ready = false;
    let disposed = false;
    const pending: { seq: number; data: Uint8Array }[] = [];
    // Output arrives coalesced. Under a flood the daemon drops the oldest undelivered bytes, which shows up as a
    // chunk that starts past the end of the previous one; the daemon's scrollback still holds them, so re-attach.
    const attach = () => {
      ready = false;
      void Terminals.attach(id).then((a) => {
        if (disposed) return;
        term.reset();
        term.write(b64(a.data));
        seq = a.seq;
        ready = true;
        const queued = pending.splice(0);
        for (const p of queued) applyChunk(p.seq, p.data);
        if (a.info.state === "exited") setExited(a.info.exitCode ?? 0);
        void Terminals.resize(id, term.cols, term.rows).catch(() => {});
      }).catch((e) => term.write(`\r\n\x1b[31m[attach failed: ${String(e)}]\x1b[0m\r\n`));
    };
    const applyChunk = (end: number, d: Uint8Array) => {
      if (!ready) { pending.push({ seq: end, data: d }); return; }
      if (end <= seq) return;
      if (end - d.length > seq) { pending.push({ seq: end, data: d }); attach(); return; }
      seq = end;
      term.write(d);
    };
    const offOut = events.terminalOutput.on((c) => {
      if (c.id !== id) return;
      const d = b64(c.data);
      applyChunk(c.seq, d);
    });
    const offChg = events.terminalChanged.on((i) => { if (i.id === id && i.state === "exited") setExited(i.exitCode ?? 0); });
    attach();

    const onData = term.onData((d) => void Terminals.write(id, d).catch(() => {}));
    const onBin = term.onBinary((d) => void Terminals.write(id, d).catch(() => {}));
    term.attachCustomKeyEventHandler((e) => {
      if (e.type !== "keydown") return true;
      if (e.ctrlKey && e.shiftKey && e.code === "KeyC") { void navigator.clipboard.writeText(term.getSelection()); return false; }
      if (e.ctrlKey && e.shiftKey && e.code === "KeyV") { void navigator.clipboard.readText().then((t) => term.paste(t)); return false; }
      // Windows Terminal convention: Ctrl+C copies when text is selected, otherwise interrupts.
      if (e.ctrlKey && !e.shiftKey && e.code === "KeyC" && term.hasSelection()) { void navigator.clipboard.writeText(term.getSelection()); term.clearSelection(); return false; }
      if (e.ctrlKey && e.code === "KeyF") { setFinding(true); return false; }
      return true;
    });
    const onResize = term.onResize(({ cols, rows }) => void Terminals.resize(id, cols, rows).catch(() => {}));
    const ro = new ResizeObserver(() => { try { fit.fit(); } catch { /* host hidden */ } });
    ro.observe(host.current!);
    return () => { themeObserver.disconnect(); ro.disconnect(); onResize.dispose(); onData.dispose(); onBin.dispose(); offOut(); offChg(); disposed = true; term.dispose(); };
  }, [id]);

  // A font size change updates the live terminal; rebuilding it would drop the view and re-attach.
  useEffect(() => {
    sizeRef.current = fontSize;
    const term = termRef.current;
    if (!term || term.options.fontSize === fontSize) return;
    term.options.fontSize = fontSize;
    try { fitRef.current?.fit(); } catch { /* host hidden */ }
  }, [fontSize]);

  useEffect(() => { if (active) termRef.current?.focus(); }, [active]);

  return (
    <div className={cn("relative flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden rounded-lg border bg-background/70", active ? "border-primary/50" : "border-border")} onMouseDown={onFocus} data-testid={`terminal-${id}`}>
      {finding && (
        <div className="absolute top-2 right-3 z-10 flex items-center gap-1 rounded-md border border-border bg-popover px-2 py-1">
          <Search className="size-3.5 text-muted-foreground" />
          <input autoFocus aria-label="Find in terminal" value={query} placeholder="Find" className="w-40 bg-transparent outline-none"
            onChange={(e) => { setQuery(e.target.value); search.current?.findNext(e.target.value, { incremental: true }); }}
            onKeyDown={(e) => { if (e.key === "Enter") { if (e.shiftKey) search.current?.findPrevious(query); else search.current?.findNext(query); } if (e.key === "Escape") { setFinding(false); termRef.current?.focus(); } }} />
          <button aria-label="Close find" onClick={() => setFinding(false)}><X className="size-3.5" /></button>
        </div>
      )}
      <div ref={host} className="min-h-0 flex-1 p-1.5" />
      {exited !== null && (
        <div role="status" className="border-t border-border bg-surface px-3 py-1 text-xs text-muted-foreground">
          Process exited with code <span className="font-mono text-foreground">{exited}</span>. This session is finished; open a new terminal to continue.
        </div>
      )}
    </div>
  );
}
