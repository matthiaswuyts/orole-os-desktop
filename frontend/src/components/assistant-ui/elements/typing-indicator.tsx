import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";

/**
 * assistant-ui's typing indicator (https://www.assistant-ui.com/elements/typing-indicator): three dots bouncing in
 * a staggered wave, bare or inside a rounded bubble. Show it from the moment a run starts until the first content lands.
 */
export function TypingIndicator({ variant = "bubble", className, ...props }: Omit<ComponentProps<"div">, "children"> & { variant?: "bubble" | "bare" }) {
  return <div role="status" aria-label="Assistant is typing" data-slot="typing-indicator" data-variant={variant} {...props}
    className={cn("inline-flex items-center gap-1", variant === "bubble" && "w-fit rounded-2xl bg-secondary px-3 py-2.5", variant === "bare" && "py-1.5", className)}>
    {[0, 1, 2].map((i) => <span key={i} aria-hidden className="size-1.5 animate-bounce rounded-full bg-muted-foreground/70 motion-reduce:animate-none" style={{ animationDelay: `${i * 150}ms` }} />)}
  </div>;
}
