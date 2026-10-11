/** Local calendar identity includes the year and does not conflate UTC and local midnight. */
export function conversationDay(at: string): string | null {
  const date = new Date(at);
  return Number.isNaN(date.getTime()) ? null : `${date.getFullYear()}-${date.getMonth() + 1}-${date.getDate()}`;
}

/** The one place the feed shows a date. Rows carry their exact time only as a hover title. */
export function DaySeparator({ at }: { at: string }) {
  const date = new Date(at);
  if (Number.isNaN(date.getTime())) return null;
  const label = date.toLocaleDateString(undefined, { weekday: "short", year: "numeric", month: "short", day: "numeric" });
  return <div role="separator" aria-label={label} data-slot="day-separator" className="my-2 flex items-center gap-3 text-xs text-muted-foreground">
    <span aria-hidden className="h-px flex-1 bg-border" /><time dateTime={at}>{label}</time><span aria-hidden className="h-px flex-1 bg-border" />
  </div>;
}
