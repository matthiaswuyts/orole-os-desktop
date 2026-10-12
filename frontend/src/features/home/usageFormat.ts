import type { DailyUsage, TokenTotals } from "@/mygo";

const UNITS = ["", "K", "M", "B", "T"] as const;
const DOLLARS = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", minimumFractionDigits: 2, maximumFractionDigits: 2 });

/**
 * Token counts as the usage cards show them: under 1,000 as is ("999"), then two decimals and a
 * unit ("1.00K", "937.70M", "1.88B"). A value that rounds up to 1,000 of a unit moves to the next
 * one, so 999,999 reads "1.00M", not "1000.00K".
 */
export function compactNumber(n: number): string {
  const rounded = Number.isFinite(n) ? Math.round(Math.max(0, n)) : 0;
  if (rounded < 1000) return String(rounded);
  let unit = Math.max(1, Math.min(Math.floor(Math.log10(rounded) / 3), UNITS.length - 1));
  if (unit < UNITS.length - 1 && Number((rounded / 1000 ** unit).toFixed(2)) >= 1000) unit++;
  return `${(rounded / 1000 ** unit).toFixed(2)}${UNITS[unit]}`;
}

/** US dollars with cents ("$586.17", "$1,204.50"); a non-zero amount under half a cent reads "<$0.01". */
export function usd(amount: number): string {
  const value = Number.isFinite(amount) ? Math.max(0, amount) : 0;
  if (value > 0 && value < 0.005) return "<$0.01";
  return DOLLARS.format(value);
}

/**
 * The spend of a token total. When some models have no price the cost is a lower bound, so it reads
 * "≥ $586.17"; a lower bound under a cent reads "≥ $0.00" (a tiny unpriced spend is not "<$0.01").
 */
export function costLabel(tokens: Pick<TokenTotals, "costUsd" | "unpriced">): string {
  if (tokens.unpriced.length === 0) return usd(tokens.costUsd);
  return `≥ ${tokens.costUsd < 0.005 ? "$0.00" : usd(tokens.costUsd)}`;
}

/**
 * Sparkline points from the 365-day series: the last 7 days for the 24h and 7-day ranges, the year
 * in weekly sums (counted back from today, so the last point is the current 7 days) for all time.
 */
export function sparkSeries(daily: readonly DailyUsage[], weekly: boolean, pick: (day: DailyUsage) => number): number[] {
  if (!weekly) return daily.slice(-7).map(pick);
  const weeks: number[] = [];
  for (let end = daily.length; end > 0; end -= 7) weeks.push(daily.slice(Math.max(0, end - 7), end).reduce((sum, day) => sum + pick(day), 0));
  return weeks.reverse();
}
