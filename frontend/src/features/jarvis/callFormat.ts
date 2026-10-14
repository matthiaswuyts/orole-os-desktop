import { PhoneIncomingIcon, PhoneMissedIcon, PhoneOutgoingIcon } from "lucide-react";
import type { CallRecord, MsgPart } from "@/mygo";

/** "1:15" for a call length in milliseconds. */
export function clock(ms: number): string {
  const s = Math.round(ms / 1000);
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

/** An estimated call cost as the daemon's card writes it: "est. $0.15" (1 USD = 1,000,000 micros). */
export function costText(micros: number): string {
  return `est. $${(micros / 1e6).toFixed(2)}`;
}

/** A stored call as the same card part the daemon puts in chat (internal/jarvis Calls.card). */
export function callPart(call: CallRecord): MsgPart {
  const durationMs = call.answeredAt && call.endedAt ? call.endedAt - call.answeredAt : 0;
  return {
    type: "call", id: call.id, name: call.direction, status: call.status, title: call.reason,
    ...(durationMs > 0 ? { durationMs } : {}),
    ...(call.estCostMicros > 0 ? { summary: costText(call.estCostMicros) } : {}),
  };
}

const MISSED = new Set(["voicemail", "no_answer", "failed", "rejected"]);
const LIVE = new Set(["queued", "dialing", "in_progress"]);

/** How a call card and the call pane name a call: who rang, and whether it is still on. */
export function callLook(part: Pick<MsgPart, "name" | "status">) {
  const missed = MISSED.has(part.status ?? "");
  const live = LIVE.has(part.status ?? "");
  const inbound = part.name === "inbound";
  const Icon = missed ? PhoneMissedIcon : inbound ? PhoneIncomingIcon : PhoneOutgoingIcon;
  const label = missed ? "Missed call" : inbound ? (live ? "You called · on the line" : "You called") : live ? "Calling you…" : "Called you";
  return { Icon, label, missed, live };
}
