/** One calendar day of the heat graph. `date` is a local-time `YYYY-MM-DD` key. */
export interface DayCell {
  date: string;
  count: number;
  level: 0 | 1 | 2 | 3 | 4;
  titles: string[];
  /** Accessible description, e.g. "2 tasks on October 10, 2026", built once with the grid. */
  label: string;
}

/** Streaks and busiest periods of the trailing year. Labels are formatted once, with the grid. */
export interface ActivityHighlights {
  /** Consecutive days with a task, ending today (or yesterday while today has none yet). */
  current: { days: number; since: string | null };
  longest: { days: number; range: string | null };
  /** Busiest calendar month in the window, e.g. "Oct 2026"; null without tasks. */
  month: { label: string; count: number } | null;
  /** Busiest weekday, e.g. "Wednesdays"; null without tasks. */
  weekday: { label: string; count: number } | null;
}

export interface ActivityGrid {
  /** Columns of seven days, Monday first. Days outside the window are null. */
  weeks: (DayCell | null)[][];
  /** Month label for the first week that starts in that month (index into weeks). */
  months: { week: number; label: string }[];
  total: number;
  highlights: ActivityHighlights;
}

const DAYS = 365;

// One formatter each: `toLocaleDateString` with options builds a new Intl formatter per call.
const DAY_LABEL = new Intl.DateTimeFormat(undefined, { month: "long", day: "numeric", year: "numeric" });
const MONTH_LABEL = new Intl.DateTimeFormat(undefined, { month: "short" });
const MONTH_YEAR = new Intl.DateTimeFormat(undefined, { month: "short", year: "numeric" });
const SHORT_DAY = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" });
const WEEKDAY = new Intl.DateTimeFormat(undefined, { weekday: "long" });

export const dayKey = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;

/** Quartile-style thresholds so one busy day does not flatten the rest. */
function levelFor(count: number, max: number): DayCell["level"] {
  if (count <= 0 || max <= 0) return 0;
  return Math.min(4, Math.ceil((count / max) * 4)) as DayCell["level"];
}

/**
 * Buckets items by local calendar day and lays out the trailing 365 days ending at `now`,
 * aligned back to Monday, with the window's streaks and busiest month and weekday.
 * Items outside the window, or with an unparseable time, are dropped.
 */
export function buildActivityGrid(items: { at: string | number; title: string }[], now: Date = new Date()): ActivityGrid {
  const end = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const start = new Date(end);
  start.setDate(start.getDate() - (DAYS - 1));
  start.setDate(start.getDate() - ((start.getDay() + 6) % 7));

  const byDay = new Map<string, string[]>();
  for (const item of items) {
    const t = new Date(item.at);
    if (Number.isNaN(t.getTime())) continue;
    const key = dayKey(t);
    const list = byDay.get(key);
    if (list) list.push(item.title); else byDay.set(key, [item.title]);
  }

  const windowStart = new Date(end);
  windowStart.setDate(windowStart.getDate() - (DAYS - 1));
  const startKey = dayKey(windowStart);
  const endKey = dayKey(end);
  let max = 0;
  let total = 0;
  for (const [key, titles] of byDay) {
    if (key < startKey || key > endKey) continue;
    max = Math.max(max, titles.length);
    total += titles.length;
  }

  const weeks: (DayCell | null)[][] = [];
  const months: ActivityGrid["months"] = [];
  // Days of the window in order, with their date, for the highlights below.
  const days: { cell: DayCell; at: Date }[] = [];
  const cursor = new Date(start);
  let lastMonth = -1;
  while (cursor <= end) {
    const column: (DayCell | null)[] = [];
    for (let i = 0; i < 7; i++) {
      const key = dayKey(cursor);
      if (cursor > end || key < startKey) column.push(null);
      else {
        const titles = byDay.get(key) ?? [];
        const cell: DayCell = { date: key, count: titles.length, level: levelFor(titles.length, max), titles, label: `${titles.length} ${titles.length === 1 ? "task" : "tasks"} on ${DAY_LABEL.format(cursor)}` };
        column.push(cell);
        days.push({ cell, at: new Date(cursor) });
      }
      if (i === 0 && cursor.getMonth() !== lastMonth && cursor >= windowStart) {
        lastMonth = cursor.getMonth();
        // The window's first partial week is labelled only if the month starts there.
        months.push({ week: weeks.length, label: MONTH_LABEL.format(cursor) });
      }
      cursor.setDate(cursor.getDate() + 1);
    }
    weeks.push(column);
  }
  return { weeks, months, total, highlights: highlightsOf(days) };
}

function highlightsOf(days: { cell: DayCell; at: Date }[]): ActivityHighlights {
  let longest = { days: 0, from: -1, to: -1 };
  let run = 0;
  const byMonth = new Map<string, { at: Date; count: number }>();
  const byWeekday = [0, 0, 0, 0, 0, 0, 0];
  days.forEach(({ cell, at }, i) => {
    run = cell.count > 0 ? run + 1 : 0;
    if (run > longest.days) longest = { days: run, from: i - run + 1, to: i };
    if (cell.count === 0) return;
    const month = cell.date.slice(0, 7);
    const entry = byMonth.get(month);
    if (entry) entry.count += cell.count; else byMonth.set(month, { at, count: cell.count });
    byWeekday[at.getDay()]! += cell.count;
  });

  // Today without a task yet does not break the streak; it only stops counting.
  let current = 0;
  let i = days.length - 1;
  if (i >= 0 && days[i]!.cell.count === 0) i--;
  for (; i >= 0 && days[i]!.cell.count > 0; i--) current++;

  let month: ActivityHighlights["month"] = null;
  for (const { at, count } of byMonth.values()) if (!month || count > month.count) month = { label: MONTH_YEAR.format(at), count };
  let weekday: ActivityHighlights["weekday"] = null;
  // Monday first, so a tie goes to the earlier day of the working week.
  for (const d of [1, 2, 3, 4, 5, 6, 0]) {
    const count = byWeekday[d]!;
    if (count > 0 && (!weekday || count > weekday.count)) weekday = { label: `${WEEKDAY.format(new Date(2024, 0, d === 0 ? 7 : d))}s`, count };
  }
  const span = (from: number, to: number) => from === to ? SHORT_DAY.format(days[from]!.at) : `${SHORT_DAY.format(days[from]!.at)} – ${SHORT_DAY.format(days[to]!.at)}`;
  return {
    current: { days: current, since: current > 0 ? SHORT_DAY.format(days[i + 1]!.at) : null },
    longest: { days: longest.days, range: longest.days > 0 ? span(longest.from, longest.to) : null },
    month,
    weekday,
  };
}

export type OverviewRange = "week" | "month" | "year";

/** What the Overview counts about one task. */
export interface TaskFact {
  startedAt: string;
  /** When the task finished (its end, else its last update); only read for completed tasks. */
  finishedAt: string;
  completed: boolean;
  pullRequest: boolean;
  /** Lines added plus lines deleted. */
  lines: number;
}

export interface RangeTotals {
  started: number;
  completed: number;
  pullRequests: number;
  lines: number;
}

/**
 * Totals for the range so far (this week from Monday, this month, this year) and for the same
 * stretch of the previous one, so a Wednesday compares with last Monday-to-Wednesday rather than
 * a whole week. Tasks count by start; completions by when they finished.
 */
export function compareRange(facts: TaskFact[], range: OverviewRange, now: Date = new Date()): { current: RangeTotals; previous: RangeTotals } {
  const y = now.getFullYear(), m = now.getMonth();
  const start = range === "year" ? new Date(y, 0, 1) : range === "month" ? new Date(y, m, 1) : new Date(y, m, now.getDate() - ((now.getDay() + 6) % 7));
  const prevStart = new Date(start), prevEnd = new Date(now);
  if (range === "week") { prevStart.setDate(prevStart.getDate() - 7); prevEnd.setDate(prevEnd.getDate() - 7); }
  else if (range === "month") {
    prevStart.setMonth(prevStart.getMonth() - 1);
    // Clamp to the previous month's length: March 31 compares with February 28/29.
    const last = new Date(now.getFullYear(), now.getMonth(), 0).getDate();
    prevEnd.setDate(1);
    prevEnd.setMonth(prevEnd.getMonth() - 1);
    prevEnd.setDate(Math.min(now.getDate(), last));
  } else {
    prevStart.setFullYear(prevStart.getFullYear() - 1);
    prevEnd.setFullYear(prevEnd.getFullYear() - 1);
  }
  const windows = [[start.getTime(), now.getTime()], [prevStart.getTime(), prevEnd.getTime()]] as const;
  const totals = windows.map(() => ({ started: 0, completed: 0, pullRequests: 0, lines: 0 }));
  for (const fact of facts) {
    const begun = Date.parse(fact.startedAt), finished = Date.parse(fact.finishedAt);
    windows.forEach(([from, to], w) => {
      const t = totals[w]!;
      if (begun >= from && begun <= to) {
        t.started++;
        if (fact.pullRequest) t.pullRequests++;
        t.lines += fact.lines;
      }
      if (fact.completed && finished >= from && finished <= to) t.completed++;
    });
  }
  return { current: totals[0]!, previous: totals[1]! };
}
