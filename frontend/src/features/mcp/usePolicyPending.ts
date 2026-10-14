import { useEffect, useState } from "react";
import { events, Policy, type Pending } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { errorText } from "./shared";

const NO_PENDING: Pending[] = [];

export interface PolicyPending {
  items: Pending[];
  /** When the queue was last confirmed, unix ms; 0 until the first snapshot arrives. */
  checkedAt: number;
  error: string | null;
  /** Fetches the queue again (after a failed load or a decision). */
  retry: () => void;
}

/** The daemon's approval queue, pushed on change; subscribes before fetching so a snapshot cannot hide a new request. */
export function usePolicyPending(): PolicyPending {
  const [state, setState] = useState<{ items: Pending[]; checkedAt: number; error: string | null }>({ items: NO_PENDING, checkedAt: 0, error: null });
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    if (!inDesktop) return;
    let live = true;
    let sequence = 0;
    const refresh = () => {
      const version = ++sequence;
      Policy.pending().then(
        (items) => { if (live && version === sequence) setState({ items: items ?? NO_PENDING, checkedAt: Date.now(), error: null }); },
        (cause: unknown) => { if (live && version === sequence) setState((s) => ({ ...s, error: errorText(cause) })); },
      );
    };
    const offPending = events.policyPending.on(({ items }) => { sequence++; setState({ items: items ?? NO_PENDING, checkedAt: Date.now(), error: null }); });
    const offDaemon = events.daemonState.on((link) => { if (link.connected) refresh(); else { sequence++; setState((s) => ({ ...s, items: NO_PENDING })); } });
    refresh();
    return () => { live = false; offPending(); offDaemon(); };
  }, [revision]);
  return { ...state, retry: () => setRevision((value) => value + 1) };
}

/** Bot requests are decided inline in that bot's conversation, not in the global dialog. */
export const isBotActor = (actor: string) => actor.startsWith("bot:");

const MAIL_DRIVE: Record<string, string> = {
  "mail.send": "Send an email", "mail.new": "Send an email", "mail.reply": "Reply to an email", "mail.trash": "Move an email to Trash",
  "drive.share": "Share a Drive file", "drive.trash": "Move a Drive file to Trash",
};

/** What a bot's held request (or a remembered rule) would do, in the user's words; the resource names the integration tool. */
export function botRequestTitle(action: string, resource = ""): string {
  if (action === "agent.start") return "Start a code task";
  if (action === "agent.prompt") return "Send a follow-up to a task";
  const named = MAIL_DRIVE[action];
  if (named) return named;
  // "mcp/<server>/<tool>" for integrations, "chat/<bot>/<tool>" for built-in tools.
  const tool = /^(?:mcp|chat)\/[^/]+\/(.+)$/.exec(resource)?.[1];
  if ((action === "mcp.tool.call" || action === "chat.tool.call") && tool) return `Use ${tool.replaceAll("_", " ")}`;
  return action;
}
