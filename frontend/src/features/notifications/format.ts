export type DayGroup = "Today" | "Yesterday" | "This week" | "Earlier";

const DAY = 86_400_000;
const startOfDay = (ms: number) => { const d = new Date(ms); d.setHours(0, 0, 0, 0); return d.getTime(); };

/** Whole calendar days between `at` (unix seconds) and now, in local time; DST-safe. */
function daysAgo(at: number, now: number): number {
  return Math.round((startOfDay(now) - startOfDay(at * 1000)) / DAY);
}

/** The list section a notification falls in, by local calendar day. */
export function dayGroup(at: number, now = Date.now()): DayGroup {
  const days = daysAgo(at, now);
  if (days <= 0) return "Today";
  if (days === 1) return "Yesterday";
  return days < 7 ? "This week" : "Earlier";
}

/** Compact age for a list row: "now", "8m", "2h" today, then "Yesterday", a weekday, or a date. */
export function shortTime(at: number, now = Date.now()): string {
  const days = daysAgo(at, now);
  if (days <= 0) {
    const s = Math.max(0, Math.floor(now / 1000 - at));
    if (s < 60) return "now";
    if (s < 3600) return `${Math.floor(s / 60)}m`;
    return `${Math.floor(s / 3600)}h`;
  }
  if (days === 1) return "Yesterday";
  const date = new Date(at * 1000);
  if (days < 7) return date.toLocaleDateString(undefined, { weekday: "short" });
  const sameYear = date.getFullYear() === new Date(now).getFullYear();
  return date.toLocaleDateString(undefined, sameYear ? { month: "short", day: "numeric" } : { month: "short", day: "numeric", year: "numeric" });
}
