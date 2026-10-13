import { memo, useMemo, useState, type ReactNode } from "react";
import { ArrowDown, ArrowUp, ChevronDown, Clock, Minus } from "lucide-react";
import { useShallow } from "zustand/react/shallow";
import { Menu, MenuPopup, MenuRadioGroup, MenuRadioItem, MenuTrigger } from "@/components/ui/menu";
import { Widget } from "@/components/widgets/Widget";
import type { WidgetProps } from "@/components/widgets/registry";
import { sessionTitle } from "@/features/agents/format";
import { cn } from "@/lib/utils";
import { useAgents } from "@/stores/agents";
import { buildActivityGrid, compareRange, type ActivityGrid, type DayCell, type OverviewRange, type RangeTotals } from "./activity";
import { useTasksStatus } from "./useTasksStatus";

const RANGES: Record<OverviewRange, { label: string; versus: string }> = {
  week: { label: "This week", versus: "vs last week" },
  month: { label: "This month", versus: "vs last month" },
  year: { label: "This year", versus: "vs last year" },
};

const LEVEL: Record<DayCell["level"], string> = {
  0: "bg-foreground/[0.06]",
  1: "bg-emerald-500/25",
  2: "bg-emerald-500/45",
  3: "bg-emerald-500/70",
  4: "bg-emerald-500",
};
const WEEKDAYS = ["Mon", "", "Wed", "", "Fri", "", ""];
const SEPARATOR = "\u0000";
const COUNT = new Intl.NumberFormat();
const COMPACT = new Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: 1 });
const tasks = (n: number) => `${COUNT.format(n)} ${n === 1 ? "task" : "tasks"}`;

/**
 * The 365-day calendar, stretched to the column width. Memoized on the grid, so hovering a day
 * (which only changes the caption below) never rebuilds its ~370 cells.
 */
const Calendar = memo(function Calendar({ grid, onHover }: { grid: ActivityGrid; onHover: (day: DayCell | null) => void }) {
  return (
    <div role="grid" aria-label="Tasks started per day, last 365 days" onMouseLeave={() => onHover(null)}
      className="grid gap-[2px] @3xl:gap-[3px]" style={{ gridTemplateColumns: `1.75rem repeat(${grid.weeks.length}, minmax(0, 1fr))`, gridTemplateRows: "auto repeat(7, auto)" }}>
      {grid.months.map((m) => <span key={m.week} aria-hidden className="h-4 whitespace-nowrap text-[11px] text-muted-foreground leading-none" style={{ gridColumn: m.week + 2, gridRow: 1 }}>{m.label}</span>)}
      {WEEKDAYS.map((d, i) => d && <span key={d} aria-hidden className="self-center text-[11px] text-muted-foreground leading-none" style={{ gridColumn: 1, gridRow: i + 2 }}>{d}</span>)}
      {grid.weeks.map((week, w) => (
        <div role="row" key={w} className="contents">
          {week.map((day, i) => day
            ? <div role="gridcell" key={day.date} aria-label={day.label} onMouseEnter={() => onHover(day)}
                className={cn("aspect-square rounded-[3px] outline-offset-1 hover:outline hover:outline-foreground/40", LEVEL[day.level])} style={{ gridColumn: w + 2, gridRow: i + 2 }} />
            : <div role="gridcell" aria-hidden key={i} style={{ gridColumn: w + 2, gridRow: i + 2 }} />)}
        </div>
      ))}
    </div>
  );
});

function Delta({ current, previous, versus }: { current: number; previous: number; versus: string }) {
  const diff = current - previous;
  const Icon = diff > 0 ? ArrowUp : diff < 0 ? ArrowDown : Minus;
  return (
    <p className="flex items-center gap-1.5 text-muted-foreground text-xs">
      <span aria-hidden className={cn("inline-flex size-4 items-center justify-center rounded-[4px]",
        diff > 0 ? "bg-emerald-500/15 text-emerald-500" : diff < 0 ? "bg-red-500/15 text-red-500" : "bg-foreground/[0.06]")}>
        <Icon className="size-3" strokeWidth={2.5} />
      </span>
      <span className="tabular-nums">{diff > 0 ? "+" : diff < 0 ? "\u2212" : "±"}{COUNT.format(Math.abs(diff))} {versus}</span>
    </p>
  );
}

function Cell({ label, value, children }: { label: string; value: ReactNode; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1.5 py-4 pr-5 @3xl:px-5 @3xl:first:pl-0">
      <p className="truncate text-muted-foreground text-xs">{label}</p>
      <p className="truncate font-semibold text-2xl tabular-nums tracking-tight">{value}</p>
      {children}
    </div>
  );
}

const STATS: { key: keyof RangeTotals; label: string; format: (n: number) => string }[] = [
  { key: "started", label: "Tasks started", format: (n) => COUNT.format(n) },
  { key: "completed", label: "Completed", format: (n) => COUNT.format(n) },
  { key: "pullRequests", label: "Pull requests", format: (n) => COUNT.format(n) },
  { key: "lines", label: "Lines changed", format: (n) => COMPACT.format(n) },
];

function RangePicker({ range, onRange }: { range: OverviewRange; onRange: (range: OverviewRange) => void }) {
  return (
    <Menu>
      <MenuTrigger render={<button type="button" aria-label={`Range: ${RANGES[range].label}`} className="inline-flex h-7 items-center gap-1.5 rounded-full border bg-card px-3 text-muted-foreground text-xs outline-none hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring data-popup-open:bg-accent" />}>
        <Clock className="size-3.5" />{RANGES[range].label}<ChevronDown className="size-3 opacity-70" />
      </MenuTrigger>
      <MenuPopup align="end" className="min-w-36">
        <MenuRadioGroup value={range} onValueChange={(value: OverviewRange) => onRange(value)}>
          {(Object.keys(RANGES) as OverviewRange[]).map((r) => <MenuRadioItem key={r} value={r}>{RANGES[r].label}</MenuRadioItem>)}
        </MenuRadioGroup>
      </MenuPopup>
    </Menu>
  );
}

/** Tasks started and finished, pull requests and lines changed for a range, then a year of activity with streaks. */
export function OverviewWidget({ config, onConfig, onRemove }: WidgetProps) {
  const range: OverviewRange = config?.range === "month" || config?.range === "year" ? config.range : "week";
  const { status, message } = useTasksStatus();
  // Each selector projects only what it shows and is compared by value, so the stream of session
  // updates (activity, tail, usage...) rebuilds neither the calendar nor the totals.
  const starts = useAgents(useShallow((s) => Object.values(s.sessions).map((session) => session.startedAt + SEPARATOR + sessionTitle(session))));
  const facts = useAgents(useShallow((s) => Object.values(s.sessions).map((session) => [
    session.startedAt, session.status === "done" ? session.endedAt || session.updatedAt : "", session.prNumber > 0 ? 1 : 0, session.additions + session.deletions,
  ].join(SEPARATOR))));
  const [hover, setHover] = useState<DayCell | null>(null);
  const grid = useMemo(() => buildActivityGrid(starts.map((entry) => {
    const split = entry.indexOf(SEPARATOR);
    return { at: entry.slice(0, split), title: entry.slice(split + 1) };
  })), [starts]);
  const totals = useMemo(() => compareRange(facts.map((entry) => {
    const [startedAt = "", finishedAt = "", pr, lines] = entry.split(SEPARATOR);
    return { startedAt, finishedAt, completed: finishedAt !== "", pullRequest: pr === "1", lines: Number(lines) || 0 };
  }), range), [facts, range]);
  const shown = hover && hover.count > 0 ? hover : null;
  const h = grid.highlights;

  return (
    <Widget title="Overview" variant="bare" status={status} message={message} onRemove={onRemove}
      actions={<RangePicker range={range} onRange={(next) => onConfig({ range: next })} />}>
      <div className="flex flex-col gap-7">
        <div className="grid grid-cols-2 divide-border @3xl:grid-cols-4 @3xl:divide-x">
          {STATS.map((stat) => (
            <Cell key={stat.key} label={stat.label} value={stat.format(totals.current[stat.key])}>
              <Delta current={totals.current[stat.key]} previous={totals.previous[stat.key]} versus={RANGES[range].versus} />
            </Cell>
          ))}
        </div>

        <div className="flex flex-col gap-3">
          <div className="flex items-center justify-between gap-3 text-xs">
            <p className="font-medium text-sm">{tasks(grid.total)} in the last year</p>
            <div className="flex shrink-0 items-center gap-1 text-muted-foreground" aria-hidden>
              Less
              {([0, 1, 2, 3, 4] as const).map((l) => <span key={l} className={cn("size-2.5 rounded-[2px]", LEVEL[l])} />)}
              More
            </div>
          </div>
          <Calendar grid={grid} onHover={setHover} />
          <p className="h-4 truncate text-muted-foreground text-xs" aria-live="polite">
            {shown && <>
              <span className="font-medium text-foreground">{shown.label}</span>
              <span className="ml-2">{shown.titles.slice(0, 3).join(" · ")}{shown.titles.length > 3 ? ` · +${shown.titles.length - 3} more` : ""}</span>
            </>}
            {!shown && grid.total === 0 && "No tasks yet. Start one above and it shows up here."}
          </p>
        </div>

        <div className="grid grid-cols-2 divide-border border-t @3xl:grid-cols-4 @3xl:divide-x">
          <Cell label="Current streak" value={`${h.current.days} ${h.current.days === 1 ? "day" : "days"}`}>
            <p className="truncate text-muted-foreground text-xs">{h.current.since ? `Since ${h.current.since}` : "Start a task to begin one"}</p>
          </Cell>
          <Cell label="Longest streak" value={`${h.longest.days} ${h.longest.days === 1 ? "day" : "days"}`}>
            <p className="truncate text-muted-foreground text-xs">{h.longest.range ?? "In the last year"}</p>
          </Cell>
          <Cell label="Most active month" value={h.month?.label ?? "—"}>
            <p className="truncate text-muted-foreground text-xs">{h.month ? tasks(h.month.count) : "No tasks yet"}</p>
          </Cell>
          <Cell label="Most active day" value={h.weekday?.label ?? "—"}>
            <p className="truncate text-muted-foreground text-xs">{h.weekday ? tasks(h.weekday.count) : "No tasks yet"}</p>
          </Cell>
        </div>
      </div>
    </Widget>
  );
}
