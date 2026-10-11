import { useEffect, useMemo, useState } from "react";
import { CalendarDays, CheckCircle2, Circle, NotebookPen, Server } from "lucide-react";
import { useDaemon } from "@/stores/daemon";
import { useUI } from "@/stores/ui";
import { fmtUptime } from "@/lib/utils";
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
      <table className="mt-2 w-full table-fixed text-center font-mono text-xs">
        <caption className="sr-only">{now.toLocaleDateString([], { month: "long", year: "numeric" })}</caption>
        <thead><tr>
          {["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"].map((day) => <th key={day} scope="col" aria-label={day} className="font-normal text-muted-foreground">{day[0]}</th>)}
        </tr></thead>
        <tbody>{Array.from({ length: Math.ceil(cells.length / 7) }, (_, week) => (
          <tr key={week}>{Array.from({ length: 7 }, (_, day) => {
            const date = cells[week * 7 + day];
            return <td key={day} aria-current={date === now.getDate() ? "date" : undefined}
              className={date === now.getDate() ? "rounded bg-primary text-primary-foreground" : "text-foreground/80"}>{date}</td>;
          })}</tr>
        ))}</tbody>
      </table>
    </Widget>
  );
}

export function SetupWidget({ onRemove }: WidgetProps) {
  const { setView, openSettings } = useUI();
  const health = useDaemon((s) => s.health);
  const items: { label: string; done: boolean; open?: () => void }[] = [
    { label: "Local daemon running", done: !!health },
    { label: "Connect GitHub", done: false, open: () => openSettings("integrations", "github") },
    { label: "Add skills", done: false, open: () => openSettings("skills") },
    { label: "Add MCP servers", done: false, open: () => openSettings("mcp") },
    { label: "Start a task", done: false, open: () => setView("agents") },
  ];
  return (
    <Widget title="Get started" icon={<CheckCircle2 />} onRemove={onRemove}>
      <ul className="space-y-0.5">
        {items.map((i) => (
          <li key={i.label}>
            <button disabled={!i.open} onClick={i.open} className="flex w-full items-center gap-2 rounded px-1 py-[3px] text-left hover:bg-accent disabled:cursor-default disabled:hover:bg-transparent">
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

export function NotesWidget({ onRemove, config, onConfig }: WidgetProps) {
  const text = typeof config?.text === "string" ? config.text : "";
  return (
    <Widget title="Notes" icon={<NotebookPen />} onRemove={onRemove}>
      <textarea aria-label="Notes" value={text} onChange={(e) => onConfig({ text: e.target.value })} onMouseDown={(e) => e.stopPropagation()}
        placeholder="Quick notes, saved with this workspace…" className="selectable h-full w-full resize-none bg-transparent text- outline-none placeholder:text-muted-foreground/60" />
    </Widget>
  );
}
