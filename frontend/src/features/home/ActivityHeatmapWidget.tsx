import { useMemo, useState } from "react";
import { CalendarDays } from "lucide-react";
import { Widget } from "@/components/widgets/Widget";
import type { WidgetProps } from "@/components/widgets/registry";
import { sessionTitle } from "@/features/agents/format";
import { cn } from "@/lib/utils";
import { useAgents } from "@/stores/agents";
import { useDaemon } from "@/stores/daemon";
import { buildActivityGrid, type DayCell } from "./activity";

const LEVEL: Record<DayCell["level"], string> = {
  0: "bg-foreground/5 border-border",
  1: "bg-blue-500/25 border-transparent",
  2: "bg-blue-500/50 border-transparent",
  3: "bg-blue-500/75 border-transparent",
  4: "bg-blue-500 border-transparent",
};
const CELL = 12;
const GAP = 3;
const WEEKDAYS = ["Mon", "", "Wed", "", "Fri", "", ""];

const describe = (d: DayCell) => {
  const date = new Date(`${d.date}T00:00:00`).toLocaleDateString(undefined, { month: "long", day: "numeric", year: "numeric" });
  return `${d.count} ${d.count === 1 ? "task" : "tasks"} on ${date}`;
};

/** GitHub-style calendar of the agent tasks started on each of the last 365 days. */
export function ActivityHeatmapWidget({ onRemove }: WidgetProps) {
  const { available, loaded, error, offline, sessions } = useAgents();
  const link = useDaemon((s) => s.link);
  const [hover, setHover] = useState<DayCell | null>(null);
  const grid = useMemo(
    () => buildActivityGrid(Object.values(sessions).map((s) => ({ at: s.startedAt, title: sessionTitle(s) }))),
    [sessions],
  );
  const status = !available ? "offline" : link === "offline" || offline ? "offline" : error ? "error" : !loaded ? "loading" : "ready";
  const message = !available ? "Open Orole OS desktop to see your activity." : status === "offline" ? "Daemon offline. Reconnect to load activity." : error ?? undefined;
  const shown = hover && hover.count > 0 ? hover : null;

  return <Widget title="Activity" icon={<CalendarDays />} status={status} message={message} onRemove={onRemove}
    count={status === "ready" ? <span className="text-muted-foreground text-xs tabular-nums">{grid.total} {grid.total === 1 ? "task" : "tasks"} in the last year</span> : undefined}>
    <div className="flex flex-col gap-2">
      <div className="overflow-x-auto pb-1">
        <div className="flex w-max flex-col" style={{ gap: GAP }}>
          <div className="relative ml-8 h-3 text-[10px] text-muted-foreground" aria-hidden>
            {grid.months.map((m) => <span key={m.week} className="absolute" style={{ left: m.week * (CELL + GAP) }}>{m.label}</span>)}
          </div>
          <div className="flex" style={{ gap: GAP }}>
            <div className="flex w-7 shrink-0 flex-col text-[10px] text-muted-foreground" style={{ gap: GAP }} aria-hidden>
              {WEEKDAYS.map((d, i) => <span key={i} className="leading-none" style={{ height: CELL }}>{d}</span>)}
            </div>
            <div role="grid" aria-label="Tasks started per day, last 365 days" className="flex" style={{ gap: GAP }} onMouseLeave={() => setHover(null)}>
              {grid.weeks.map((week, w) => (
                <div role="row" key={w} className="flex flex-col" style={{ gap: GAP }}>
                  {week.map((day, i) => day
                    ? <div role="gridcell" key={day.date} aria-label={describe(day)} title={describe(day)} onMouseEnter={() => setHover(day)}
                        className={cn("rounded-[3px] border", LEVEL[day.level])} style={{ width: CELL, height: CELL }} />
                    : <div role="gridcell" aria-hidden key={i} style={{ width: CELL, height: CELL }} />)}
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>
      <div className="flex items-center justify-between gap-2 text-muted-foreground text-xs">
        <div className="min-h-8 min-w-0 flex-1" aria-live="polite">
          {shown && <>
            <p className="font-medium text-foreground">{describe(shown)}</p>
            <p className="truncate">{shown.titles.slice(0, 3).join(" · ")}{shown.titles.length > 3 ? ` · +${shown.titles.length - 3} more` : ""}</p>
          </>}
          {!shown && grid.total === 0 && <p>No tasks yet. Start one above and it shows up here.</p>}
        </div>
        <div className="flex shrink-0 items-center gap-1" aria-hidden>
          Less
          {([0, 1, 2, 3, 4] as const).map((l) => <span key={l} className={cn("rounded-[3px] border", LEVEL[l])} style={{ width: CELL, height: CELL }} />)}
          More
        </div>
      </div>
    </div>
  </Widget>;
}
