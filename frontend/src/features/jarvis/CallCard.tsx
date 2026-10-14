import { Bubble } from "@/features/chat/parts/Bubble";
import { cn } from "@/lib/utils";
import type { MsgPart } from "@/mygo";
import { useChat } from "@/stores/chat";
import { callLook, clock } from "./callFormat";

/**
 * A phone call between the owner and a bot: who rang, why, how long and the estimated cost. Drawn as one of the bot's
 * chat bubbles; clicking it opens the call's transcript beside the conversation (the spoken turns are not in the thread).
 */
export function CallCard({ part }: { part: MsgPart }) {
  const { Icon, label, missed } = callLook(part);
  const meta = [part.durationMs ? clock(part.durationMs) : "", part.summary ?? ""].filter(Boolean).join(" · ");
  const id = part.id ?? "";
  const selected = useChat((s) => id !== "" && s.pane?.kind === "call" && s.pane.id === id);
  return (
    <Bubble className={cn("p-0", selected && "ring-1 ring-ring/60")}>
      <button type="button" data-slot="call-card" aria-pressed={selected} disabled={!id} onClick={() => useChat.getState().openCallPane(id)}
        className="flex w-full min-w-56 items-center gap-3 rounded-[inherit] px-3 py-1.5 text-start outline-none transition-colors hover:bg-foreground/5 focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none motion-reduce:transition-none">
        <Icon aria-hidden className={cn("size-4 shrink-0", missed ? "text-destructive-foreground" : "text-muted-foreground")} />
        <span className="flex min-w-0 flex-col">
          <span className="font-medium">{label}</span>
          {part.title && <span className="truncate text-muted-foreground">{part.title}</span>}
        </span>
        {meta && <span className="ml-auto shrink-0 text-xs text-muted-foreground tabular-nums">{meta}</span>}
      </button>
    </Bubble>
  );
}
