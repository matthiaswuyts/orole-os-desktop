import type * as React from "react";
import { platformKeys } from "@/lib/platform";
import { cn } from "@/lib/utils";

// A key hint as a string is written for this platform (lib/platform.ts):
// ⌘K on a Mac, Ctrl+K on Linux.
export function Kbd({
  className,
  children,
  ...props
}: React.ComponentProps<"kbd">): React.ReactElement {
  return (
    <kbd
      className={cn(
        "pointer-events-none inline-flex h-5 min-w-5 select-none items-center justify-center gap-1 rounded-[.25rem] bg-muted px-1 font-medium font-sans text-muted-foreground text-xs [&_svg:not([class*='size-'])]:size-3",
        className,
      )}
      data-slot="kbd"
      {...props}
    >
      {typeof children === "string" ? platformKeys(children) : children}
    </kbd>
  );
}

export function KbdGroup({
  className,
  ...props
}: React.ComponentProps<"kbd">): React.ReactElement {
  return (
    <kbd
      className={cn("inline-flex items-center gap-1", className)}
      data-slot="kbd-group"
      {...props}
    />
  );
}
