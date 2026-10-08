import { useEffect, useMemo, useRef, useState } from "react";
import { Activity } from "lucide-react";
import { System, type HostMetrics } from "@/mygo";
import { AreaChart, LineChart } from "@/components/dither-kit/area-chart";
import { Area, Line } from "@/components/dither-kit/area";
import { YAxis } from "@/components/dither-kit/y-axis";
import type { ChartConfig } from "@/components/dither-kit/chart-context";
import { Widget } from "@/components/widgets/Widget";
import { inDesktop } from "@/lib/backend";
import { useDaemon } from "@/stores/daemon";

const CPU_CONFIG = { value: { label: "Host CPU", color: "blue" } } satisfies ChartConfig;
const MEMORY_CONFIG = { value: { label: "Host memory used", color: "purple" } } satisfies ChartConfig;
const HEAP_CONFIG = { value: { label: "Daemon heap", color: "green" } } satisfies ChartConfig;
const MARGINS = { top: 8, right: 3, bottom: 6, left: 38 };
const percent = (value: number) => `${value.toFixed(0)}%`;
const numeric = (value: number) => String(Number(value.toFixed(1)));
const bytes = (value: number) => value >= 1024 ** 3
  ? `${(value / 1024 ** 3).toFixed(1)} GiB`
  : `${(value / 1024 ** 2).toFixed(1)} MiB`;
const memoryUsed = (sample: HostMetrics) => sample.memoryTotalBytes - sample.memoryAvailableBytes;
const memoryPercent = (sample: HostMetrics) => sample.memoryTotalBytes > 0
  ? (memoryUsed(sample) / sample.memoryTotalBytes) * 100 : null;
const uptime = (seconds: number) => {
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);
  return days ? `${days}d ${hours % 24}h` : hours ? `${hours}h ${minutes % 60}m` : `${minutes}m`;
};
type HeapSample = { sampledAt: string; value: number; pid: number };
type Point = { value: number };

/** Local, bounded presentation history; the daemon owns sampling and caching. */
export function HostActivity({ onRemove }: { onRemove: () => void }) {
  const { link, health, refreshedAt, error: daemonError } = useDaemon();
  const container = useRef<HTMLDivElement>(null);
  const [active, setActive] = useState(() => !document.hidden && document.hasFocus());
  const [samples, setSamples] = useState<HostMetrics[]>([]);
  const [heap, setHeap] = useState<HeapSample[]>([]);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let intersecting = true;
    const update = () => setActive(intersecting && !document.hidden && document.hasFocus());
    const observer = new IntersectionObserver(([entry]) => {
      intersecting = entry?.isIntersecting ?? false;
      update();
    });
    if (container.current) observer.observe(container.current);
    document.addEventListener("visibilitychange", update);
    window.addEventListener("focus", update);
    window.addEventListener("blur", update);
    update();
    return () => {
      observer.disconnect();
      document.removeEventListener("visibilitychange", update);
      window.removeEventListener("focus", update);
      window.removeEventListener("blur", update);
    };
  }, []);

  useEffect(() => {
    if (!inDesktop || !active || link !== "connected") return;
    let live = true;
    let pending = false;
    const sample = async () => {
      if (pending) return;
      pending = true;
      try {
        const next = await System.metrics();
        if (!live) return;
        setError(null);
        setSamples((previous) => previous.at(-1)?.sampledAt === next.sampledAt
          ? previous : [...previous.slice(-59), next]);
      } catch (cause) {
        if (live) setError(cause instanceof Error ? cause.message : String(cause));
      } finally {
        pending = false;
      }
    };
    void sample();
    const timer = window.setInterval(() => void sample(), 5_000);
    return () => { live = false; window.clearInterval(timer); };
  }, [active, link]);

  useEffect(() => {
    if (!active || link !== "connected" || !health || !refreshedAt) return;
    const sampledAt = new Date(refreshedAt).toISOString();
    setHeap((previous) => {
      if (previous.at(-1)?.sampledAt === sampledAt) return previous;
      // Do not draw a single timeline across a daemon process restart.
      const history = previous.at(-1)?.pid === health.pid ? previous : [];
      return [...history.slice(-59), { sampledAt, value: health.memAllocMB, pid: health.pid }];
    });
  }, [active, link, health, refreshedAt]);

  const latest = samples.at(-1);
  const latestHeap = heap.at(-1);
  // An unavailable CPU interval is absent, not a fabricated zero-percent point.
  const cpu = useMemo(() => samples.filter((sample) => sample.cpuReady)
    .map((sample) => ({ value: sample.cpuPercent })), [samples]);
  const memory = useMemo(() => samples.flatMap((sample) => {
    const value = memoryPercent(sample);
    return value === null ? [] : [{ value }];
  }), [samples]);
  const status = !inDesktop || link === "preview" ? "offline"
    : link === "offline" ? "error" : "ready";
  const message = !inDesktop ? "Open Orole OS desktop to monitor this host." : daemonError ?? "Daemon disconnected.";

  return (
    <div ref={container} className="h-full min-h-0">
      <Widget title="System activity" icon={<Activity />} status={status} message={message} onRemove={onRemove}>
        <div className="flex flex-col gap-3">
          <p role="status" className="text-xs text-muted-foreground">
            {!active ? "Paused while hidden or unfocused. Showing last samples."
              : error ? "Host metrics unavailable. Showing last successful samples."
                : link !== "connected" ? "Waiting for the daemon…"
                  : !latest ? "Reading host metrics…" : "Sampling every 5 seconds while visible and focused."}
          </p>
          {error && <p className="break-words text-xs text-destructive-foreground">{error}</p>}
          <div className="grid grid-cols-2 gap-3">
            <figure className="min-w-0">
              <figcaption className="flex flex-wrap items-baseline justify-between gap-x-2 text-xs">
                <span>Host CPU</span>
                <span className="font-mono tabular-nums">{latest?.cpuReady ? `${latest.cpuPercent.toFixed(1)}%` : "Unavailable"}</span>
              </figcaption>
              <HistoryChart data={cpu} config={CPU_CONFIG} />
              <p className="text-xs text-muted-foreground">{latest ? `${latest.logicalCPUs} logical CPUs` : "No sample yet"}{latest && !latest.cpuReady ? " · waiting for an interval" : ""}</p>
            </figure>
            <figure className="min-w-0">
              <figcaption className="flex flex-wrap items-baseline justify-between gap-x-2 text-xs">
                <span>Host memory</span>
                <span className="font-mono tabular-nums">{latest && memoryPercent(latest) !== null ? `${memoryPercent(latest)!.toFixed(1)}%` : "Unavailable"}</span>
              </figcaption>
              <HistoryChart data={memory} config={MEMORY_CONFIG} />
              <p className="text-xs text-muted-foreground">{latest ? `${bytes(memoryUsed(latest))} / ${bytes(latest.memoryTotalBytes)} used` : "No sample yet"}</p>
            </figure>
          </div>
          {latest && <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
            <dt className="text-muted-foreground">Disk free</dt>
            <dd className="text-right font-mono tabular-nums">{bytes(latest.diskAvailableBytes)} / {bytes(latest.diskTotalBytes)}</dd>
            <dt className="text-muted-foreground">Volume path</dt>
            <dd className="selectable break-all text-right">{latest.diskPath}</dd>
            <dt className="text-muted-foreground">Host uptime</dt>
            <dd className="text-right font-mono tabular-nums">{uptime(latest.uptimeSeconds)}</dd>
            <dt className="text-muted-foreground">Last host sample</dt>
            <dd className="text-right"><time dateTime={latest.sampledAt}>{new Date(latest.sampledAt).toLocaleTimeString()}</time></dd>
          </dl>}
          <figure>
            <figcaption className="flex items-baseline justify-between gap-2 text-xs">
              <span>Daemon heap <span className="text-muted-foreground">· not host RAM</span></span>
              <span className="font-mono tabular-nums">{latestHeap ? `${latestHeap.value.toFixed(1)} MB` : "Unavailable"}</span>
            </figcaption>
            <HistoryChart data={heap} config={HEAP_CONFIG} line />
          </figure>
          <details className="text-xs">
            <summary className="min-h-6 cursor-pointer rounded focus-visible:outline-2 focus-visible:outline-ring focus-visible:outline-offset-2">Sample history ({samples.length} host, {heap.length} daemon)</summary>
            <p className="my-2 text-muted-foreground">Up to 60 real samples per source, oldest first. Charts follow sample order with automatic vertical scales; paused and unavailable intervals are not filled in. No history is recorded while this widget is hidden or unfocused.</p>
            <div className="overflow-x-auto">
              <table className="w-full text-left tabular-nums">
                <caption className="py-2 text-left font-medium">Host CPU and memory history</caption>
                <thead><tr><th scope="col" className="pr-3">Sample time</th><th scope="col" className="pr-3">CPU</th><th scope="col">Memory used / total</th></tr></thead>
                <tbody>{samples.map((sample) => <tr key={sample.sampledAt}>
                  <th scope="row" className="whitespace-nowrap py-1 pr-3 font-normal"><time dateTime={sample.sampledAt}>{new Date(sample.sampledAt).toLocaleString()}</time></th>
                  <td className="pr-3">{sample.cpuReady ? `${sample.cpuPercent.toFixed(1)}%` : "Unavailable"}</td>
                  <td className="whitespace-nowrap">{bytes(memoryUsed(sample))} / {bytes(sample.memoryTotalBytes)}</td>
                </tr>)}</tbody>
              </table>
              <table className="w-full text-left tabular-nums">
                <caption className="py-2 text-left font-medium">Daemon heap history (MB)</caption>
                <thead><tr><th scope="col" className="pr-3">Received at</th><th scope="col">Heap</th></tr></thead>
                <tbody>{heap.map((sample) => <tr key={sample.sampledAt}>
                  <th scope="row" className="whitespace-nowrap py-1 pr-3 font-normal"><time dateTime={sample.sampledAt}>{new Date(sample.sampledAt).toLocaleString()}</time></th>
                  <td>{sample.value.toFixed(1)} MB</td>
                </tr>)}</tbody>
              </table>
            </div>
          </details>
        </div>
      </Widget>
    </div>
  );
}

function HistoryChart({ data, config, line = false }: { data: Point[]; config: ChartConfig; line?: boolean }) {
  if (data.length < 2) return <p className="flex h-16 items-center text-xs text-muted-foreground">{data.length ? "Collecting history…" : "No history yet"}</p>;
  const Chart = line ? LineChart : AreaChart;
  const Series = line ? Line : Area;
  // Text latest values and the keyboard-accessible tables are the equivalent
  // alternative. The canvas is decorative and performs no idle animation.
  return <div aria-hidden="true" className="pointer-events-none h-16">
    <Chart data={data} config={config} animate={false} interactive={false} bloom="off" margins={MARGINS}>
      <YAxis tickCount={2} tickMargin={4} tickFormatter={line ? numeric : percent} />
      <Series dataKey="value" />
    </Chart>
  </div>;
}
