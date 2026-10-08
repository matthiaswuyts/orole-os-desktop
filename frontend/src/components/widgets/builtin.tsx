import { useEffect, useMemo, useState } from "react";
import { Activity, CalendarDays, CheckCircle2, Circle, NotebookPen, SquareTerminal, Server } from "lucide-react";
import { Terminals, events, type Info } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { useDaemon } from "@/stores/daemon";
import { useUI } from "@/stores/ui";
import { Widget } from "./Widget";
import type { WidgetProps } from "./registry";

function Row({ k, v }: { k: string; v: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-3 py-[3px]">
      <dt className="text-muted-foreground">{k}</dt>
      <dd className="truncate font-mono text-xs">{v}</dd>
    </div>
  );
}

export function DaemonHealthWidget({ onRemove }: WidgetProps) {
  const { link, health, error, refresh } = useDaemon();
  const status = link === "preview" ? "offline" : link === "unknown" ? "loading" : link === "offline" ? "error" : "ready";
  const message = link === "preview" ? "Preview mode: open Orole OS desktop to see the daemon." : error ?? undefined;
  return (
    <Widget title="Daemon" icon={<Server />} status={status} message={message} onRemove={onRemove}
      action={link === "offline" ? { label: "Retry", onClick: () => void refresh() } : undefined}>
      {health && (
        <dl>
          <Row k="State" v={<span className="text-success-foreground">● running</span>} />
          <Row k="Version" v={health.version} />
          <Row k="PID" v={health.pid} />
          <Row k="Uptime" v={fmtUptime(health.uptimeSec)} />
          <Row k="Memory" v={`${health.memAllocMB.toFixed(1)} MB`} />
          <Row k="Goroutines" v={health.goroutines} />
          <Row k="Schema" v={`v${health.dbVersion}`} />
          <Row k="IPC protocol" v={`v${health.protocolVersion}`} />
          <Row k="Platform" v={health.platform} />
        </dl>
      )}
    </Widget>
  );
}

export const fmtUptime = (s: number) => {
  const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
  return d ? `${d}d ${h}h` : h ? `${h}h ${m}m` : m ? `${m}m ${s % 60}s` : `${s}s`;
};

export function ClockWidget({ onRemove }: WidgetProps) {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    // Align to the next minute; no per-second timer needed for a minute-resolution clock.
    let t: ReturnType<typeof setTimeout>;
    const tick = () => {
      setNow(new Date());
      t = setTimeout(tick, 60_000 - (Date.now() % 60_000) + 50);
    };
    t = setTimeout(tick, 60_000 - (Date.now() % 60_000) + 50);
    return () => clearTimeout(t);
  }, []);
  const cells = useMemo(() => {
    const y = now.getFullYear(), m = now.getMonth();
    const first = (new Date(y, m, 1).getDay() + 6) % 7;
    const days = new Date(y, m + 1, 0).getDate();
    return [...Array<null>(first).fill(null), ...Array.from({ length: days }, (_, i) => i + 1)];
  }, [now]);
  return (
    <Widget title="Clock & calendar" icon={<CalendarDays />} onRemove={onRemove}>
      <div className="flex items-baseline gap-2">
        <time dateTime={now.toISOString()} className="text-2xl font-light tabular-nums">{now.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}</time>
        <span className="text-muted-foreground">{now.toLocaleDateString([], { weekday: "long", day: "numeric", month: "long" })}</span>
      </div>
      <div className="mt-2 grid grid-cols-7 gap-y-0.5 text-center font-mono text-xs" role="grid" aria-label={now.toLocaleDateString([], { month: "long", year: "numeric" })}>
        {["M", "T", "W", "T", "F", "S", "S"].map((d, i) => <span key={i} className="text-muted-foreground/70">{d}</span>)}
        {cells.map((c, i) => (
          <span key={i} role="gridcell" aria-current={c === now.getDate() ? "date" : undefined}
            className={c === now.getDate() ? "rounded bg-primary text-primary-foreground" : "text-foreground/80"}>{c}</span>
        ))}
      </div>
    </Widget>
  );
}

export function SetupWidget({ onRemove }: WidgetProps) {
  const setView = useUI((s) => s.setView);
  const health = useDaemon((s) => s.health);
  const items: { label: string; done: boolean; view?: Parameters<typeof setView>[0] }[] = [
    { label: "Local daemon running", done: !!health },
    { label: "Connect GitHub", done: false, view: "github" },
    { label: "Connect Orole Mail", done: false, view: "mail" },
    { label: "Connect Orole Drive", done: false, view: "drive" },
    { label: "Configure a coding agent", done: false, view: "agents" },
    { label: "Add a remote machine", done: false, view: "machines" },
  ];
  return (
    <Widget title="Get started" icon={<CheckCircle2 />} onRemove={onRemove}>
      <ul className="space-y-0.5">
        {items.map((i) => (
          <li key={i.label}>
            <button disabled={!i.view} onClick={() => i.view && setView(i.view)} className="flex w-full items-center gap-2 rounded px-1 py-[3px] text-left hover:bg-accent disabled:cursor-default disabled:hover:bg-transparent">
              {i.done ? <CheckCircle2 className="size-3.5 text-success-foreground" /> : <Circle className="size-3.5 text-muted-foreground/60" />}
              <span className={i.done ? "text-muted-foreground line-through" : ""}>{i.label}</span>
            </button>
          </li>
        ))}
      </ul>
      <p className="mt-2 text-xs text-muted-foreground">Everything is optional. Orole OS works without any of these.</p>
    </Widget>
  );
}

export function TerminalsWidget({ onRemove }: WidgetProps) {
  const setView = useUI((s) => s.setView);
  const [list, setList] = useState<Info[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  useEffect(() => {
    if (!inDesktop) return;
    let live = true;
    Terminals.list().then((l) => live && setList(l), (e) => live && setErr(String(e)));
    const o1 = events.terminalChanged.on((i) => setList((l) => [...(l ?? []).filter((x) => x.id !== i.id), i]));
    const o2 = events.terminalRemoved.on(({ id }) => setList((l) => (l ?? []).filter((x) => x.id !== id)));
    return () => { live = false; o1(); o2(); };
  }, []);
  const status = !inDesktop ? "offline" : err ? "error" : list === null ? "loading" : "ready";
  return (
    <Widget title="Terminals" icon={<SquareTerminal />} status={status} message={!inDesktop ? "Preview mode: no daemon." : err ?? undefined} onRemove={onRemove}
      action={{ label: "Open terminals", onClick: () => setView("terminals") }}>
      {list && list.length === 0 ? (
        <div className="flex h-full flex-col items-center justify-center gap-2 text-muted-foreground">
          <p>No terminals running.</p>
          <button className="rounded border border-border px-2 py-1 hover:bg-accent" onClick={() => setView("terminals")}>Open a terminal</button>
        </div>
      ) : (
        <ul className="space-y-0.5">
          {list?.map((t) => (
            <li key={t.id}>
              <button onClick={() => setView("terminals")} className="flex w-full items-center gap-2 rounded px-1 py-[3px] hover:bg-accent">
                <span className={t.state === "running" ? "text-success-foreground" : "text-muted-foreground"}>●</span>
                <span className="flex-1 truncate text-left">{t.title}</span>
                <span className="font-mono text-xs text-muted-foreground">{t.state === "running" ? `pid ${t.pid}` : `exit ${t.exitCode ?? "?"}`}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </Widget>
  );
}

export function NotesWidget({ onRemove, config, onConfig }: WidgetProps) {
  const text = typeof config?.text === "string" ? config.text : "";
  return (
    <Widget title="Notes" icon={<NotebookPen />} onRemove={onRemove}>
      <textarea aria-label="Notes" value={text} onChange={(e) => onConfig({ text: e.target.value })} onMouseDown={(e) => e.stopPropagation()}
        placeholder="Quick notes, saved with this workspace…" className="selectable h-full w-full resize-none bg-transparent text- outline-none placeholder:text-muted-foreground/60" />
    </Widget>
  );
}

export function ActivityWidget({ onRemove }: WidgetProps) {
  const health = useDaemon((s) => s.health);
  const [samples, setSamples] = useState<number[]>([]);
  useEffect(() => {
    if (health) setSamples((s) => [...s.slice(-59), health.memAllocMB]);
  }, [health]);
  const max = Math.max(1, ...samples);
  return (
    <Widget title="System activity" icon={<Activity />} status={health ? "ready" : inDesktop ? "loading" : "offline"} message={!inDesktop ? "Preview mode: no daemon." : undefined} onRemove={onRemove}>
      <p className="text-muted-foreground">Daemon memory</p>
      <svg viewBox="0 0 120 32" className="mt-1 h-16 w-full" preserveAspectRatio="none" role="img" aria-label="Daemon memory over the last samples">
        <polyline fill="none" stroke="var(--primary)" strokeWidth="1" vectorEffect="non-scaling-stroke"
          points={samples.map((v, i) => `${(i / Math.max(1, samples.length - 1)) * 120},${32 - (v / max) * 30 - 1}`).join(" ")} />
      </svg>
      <p className="font-mono text-xs text-muted-foreground">{samples.length ? `${samples[samples.length - 1]!.toFixed(1)} MB · ${samples.length} samples` : "waiting for samples"}</p>
    </Widget>
  );
}
