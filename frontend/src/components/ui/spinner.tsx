import { Snake } from "loading-dev";
import type React from "react";
import { cn } from "@/lib/utils";

type SpinnerProps = Omit<React.ComponentProps<"span">, "children"> & {
  /** Pixel size of the snake. @default 16 */
  size?: number;
};

/** The app's loading indicator (loading.dev "Snake"). Not for AI work in progress, which has its own glyphs. */
export function Spinner({ className, size = 16, ...props }: SpinnerProps): React.ReactElement {
  return (
    <span
      aria-label="Loading"
      className={cn("inline-flex shrink-0 items-center justify-center", className)}
      role="status"
      {...props}
    >
      <Snake size={size} />
    </span>
  );
}
