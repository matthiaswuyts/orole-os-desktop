/** Shared class tokens of the assistant-ui elements, so retheming one place rethemes them all. */
export const surfaces = {
  /** a bordered card */
  paper: "rounded-xl border bg-card text-card-foreground",
  /** an inset rounded field, such as a query pill or a chip */
  field: "rounded-full border bg-muted/40",
  /** small monospaced meta text */
  mono: "font-mono text-muted-foreground text-xs tabular-nums",
  /** an icon button without chrome */
  ghostButton: "inline-flex items-center justify-center rounded-full text-muted-foreground outline-none hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring",
} as const;

export const { paper, field, mono, ghostButton } = surfaces;
