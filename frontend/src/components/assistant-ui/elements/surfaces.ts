/** Shared class tokens of the assistant-ui elements, so retheming one place rethemes them all. */
export const surfaces = {
  /** a bordered card */
  paper: "rounded-xl border bg-card text-card-foreground",
  /** what a bot puts in a chat (a message, a table, a card): its chat bubble, without a border */
  bubble: "rounded-2xl rounded-bl-md bg-secondary text-secondary-foreground",
  /** an AI composer: one card for the text and its controls, lifted by a hairline and a soft shadow that firm up on focus */
  composerFrame: "rounded-2xl bg-card text-card-foreground shadow-[0_0_0_1px_var(--border),0_1px_2px_-1px_rgb(0_0_0/0.06),0_2px_4px_0_rgb(0_0_0/0.04)] transition-shadow duration-150 ease-out focus-within:shadow-[0_0_0_1px_color-mix(in_oklab,var(--ring)_70%,transparent),0_1px_2px_-1px_rgb(0_0_0/0.08),0_2px_6px_0_rgb(0_0_0/0.06)] motion-reduce:transition-none",
  /** a menu that opens from a composer: floats above it on a hairline and a deeper shadow */
  menuPopup: "rounded-2xl bg-popover text-popover-foreground shadow-[0_0_0_1px_var(--border),0_2px_4px_-2px_rgb(0_0_0/0.08),0_8px_24px_-6px_rgb(0_0_0/0.3)]",
  /** an inset rounded field, such as a query pill or a chip */
  field: "rounded-full border bg-muted/40",
  /** small monospaced meta text */
  mono: "font-mono text-muted-foreground text-xs tabular-nums",
  /** an icon button without chrome */
  ghostButton: "inline-flex items-center justify-center rounded-full text-muted-foreground outline-none hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring",
} as const;

export const { paper, bubble, composerFrame, menuPopup, field, mono, ghostButton } = surfaces;
