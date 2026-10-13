import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

/** One chat bubble of a bot: a short message from a colleague, as wide as its content up to most of the thread. */
export function Bubble({ children, className }: { children: ReactNode; className?: string }) {
  return <div data-slot="chat-bubble" className={cn("w-fit max-w-[92%] min-w-0 rounded-2xl rounded-bl-md bg-secondary px-3 py-1.5 text-sm leading-relaxed text-secondary-foreground", className)}>{children}</div>;
}
