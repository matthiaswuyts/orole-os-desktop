/** One calendar day of the heat graph. `date` is a local-time `YYYY-MM-DD` key. */
export interface DayCell {
  date: string;
  count: number;
  level: 0 | 1 | 2 | 3 | 4;
  titles: string[];
  /** Accessible description, e.g. "2 tasks on October 10, 2026", built once with the grid. */
  label: string;
}

export interface ActivityGrid {
  /** Columns of seven days, Monday first. Days outside the window are null. */
  weeks: (DayCell | null)[][];
  /** Month label for the first week that starts in that month (index into weeks). */
  months: { week: number; label: string }[];
  total: number;
}

const DAYS = 365;

// One formatter each: `toLocaleDateString` with options builds a new Intl formatter per call.
const DAY_LABEL = new Intl.DateTimeFormat(undefined, { month: "long", day: "numeric", year: "numeric" });
const MONTH_LABEL = new Intl.DateTimeFormat(undefined, { month: "short" });

export const dayKey = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;

/** Quartile-style thresholds so one busy day does not flatten the rest. */
function levelFor(count: number, max: number): DayCell["level"] {
  if (count <= 0 || max <= 0) return 0;
  return Math.min(4, Math.ceil((count / max) * 4)) as DayCell["level"];
}

/**
 * Buckets items by local calendar day and lays out the trailing 365 days ending at `now`,
 * aligned back to Monday. Items outside the window, or with an unparseable time, are dropped.
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
  const cursor = new Date(start);
  let lastMonth = -1;
  while (cursor <= end) {
    const column: (DayCell | null)[] = [];
    for (let i = 0; i < 7; i++) {
      const key = dayKey(cursor);
      if (cursor > end || key < startKey) column.push(null);
      else {
        const titles = byDay.get(key) ?? [];
        column.push({ date: key, count: titles.length, level: levelFor(titles.length, max), titles, label: `${titles.length} ${titles.length === 1 ? "task" : "tasks"} on ${DAY_LABEL.format(cursor)}` });
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
  return { weeks, months, total };
}
