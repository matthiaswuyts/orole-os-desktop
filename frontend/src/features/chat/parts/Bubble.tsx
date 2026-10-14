import type { ReactNode } from "react";
import { bubble } from "@/components/assistant-ui/elements/surfaces";
import { cn } from "@/lib/utils";

/** One chat bubble of a bot: a short message from a colleague, as wide as its content up to most of the thread. */
export function Bubble({ children, className }: { children: ReactNode; className?: string }) {
  return <div data-slot="chat-bubble" className={cn(bubble, "w-fit max-w-[92%] min-w-0 px-3 py-1.5 text-sm leading-relaxed", className)}>{children}</div>;
}
