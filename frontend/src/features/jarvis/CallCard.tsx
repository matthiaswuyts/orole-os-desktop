import { PhoneIncomingIcon, PhoneMissedIcon, PhoneOutgoingIcon } from "lucide-react";
import type { CallRecord, MsgPart } from "@/mygo";

function clock(ms: number): string {
  const s = Math.round(ms / 1000);
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

/** A stored call as the same card part the daemon puts in chat (internal/jarvis Calls.card). */
export function callPart(call: CallRecord): MsgPart {
  const durationMs = call.answeredAt && call.endedAt ? call.endedAt - call.answeredAt : 0;
  return {
    type: "call", id: call.id, name: call.direction, status: call.status, title: call.reason,
    ...(durationMs > 0 ? { durationMs } : {}),
    ...(call.estCostMicros > 0 ? { summary: `est. $${(call.estCostMicros / 1e6).toFixed(2)}` } : {}),
  };
}

/** A phone call between the owner and a bot: who rang, why, how long and the estimated cost. */
export function CallCard({ part }: { part: MsgPart }) {
  const missed = part.status === "voicemail" || part.status === "no_answer" || part.status === "failed" || part.status === "rejected";
  const live = part.status === "queued" || part.status === "dialing" || part.status === "in_progress";
  const inbound = part.name === "inbound";
  const Icon = missed ? PhoneMissedIcon : inbound ? PhoneIncomingIcon : PhoneOutgoingIcon;
  const label = missed ? "Missed call" : inbound ? (live ? "You called · on the line" : "You called") : live ? "Calling you…" : "Called you";
  const meta = [part.durationMs ? clock(part.durationMs) : "", part.summary ?? ""].filter(Boolean).join(" · ");
  return (
    <div data-slot="call-card" className="flex items-center gap-3 rounded-lg border bg-card px-3 py-2 text-sm">
      <Icon aria-hidden className={missed ? "size-4 shrink-0 text-destructive-foreground" : "size-4 shrink-0 text-muted-foreground"} />
      <div className="min-w-0">
        <p className="font-medium">{label}</p>
        {part.title && <p className="truncate text-muted-foreground">{part.title}</p>}
      </div>
      {meta && <p className="ml-auto shrink-0 text-xs text-muted-foreground">{meta}</p>}
    </div>
  );
}
