import type { DirectiveFormatter, DirectiveSegment, TriggerItem } from "@assistant-ui/react";
import { useEffect, useMemo, useState } from "react";
import { Skills, type Bot } from "@/mygo";
import { useChat } from "@/stores/chat";

/** What the user can @mention in a bot's composer: another bot, or one of this bot's own skills. */
export interface MentionTarget {
  id: string;
  type: "bot" | "skill";
  label: string;
  avatar: string;
}

/** Most mentions the daemon accepts per message (internal/chat/mention.go). */
export const MAX_BOT_MENTIONS = 3;
export const MAX_SKILL_MENTIONS = 5;

const WORD = /[\p{L}\p{N}_-]/u;

/** The character that opens each kind's picker and prefixes it in the text: `@Bot`, `/skill`. */
export const TRIGGER = { bot: "@", skill: "/" } as const;

/** One mention found in composer text: `text.slice(start, end)` is `@Name` or `/name`. */
export interface MentionSpan { start: number; end: number; target: MentionTarget }

/** Where the known bots and skills are mentioned in `text`: a trigger at the start or after whitespace, then the exact name. */
export function mentionSpans(text: string, targets: readonly MentionTarget[]): MentionSpan[] {
  // Longest names first so "@Research Bot" is not read as the shorter bot "Research".
  const known = targets.toSorted((a, b) => b.label.length - a.label.length);
  const spans: MentionSpan[] = [];
  for (let i = 0; i < text.length; i++) {
    const trigger = text[i];
    if ((trigger !== TRIGGER.bot && trigger !== TRIGGER.skill) || (i > 0 && !/\s/.test(text[i - 1]!))) continue;
    const kind = trigger === TRIGGER.skill ? "skill" : "bot";
    const hit = known.find((t) => {
      const end = i + 1 + t.label.length;
      return t.type === kind && text.startsWith(t.label, i + 1) && (end >= text.length || !WORD.test(text[end]!));
    });
    if (hit) {
      spans.push({ start: i, end: i + 1 + hit.label.length, target: hit });
      i += hit.label.length;
    }
  }
  return spans;
}

/**
 * What Backspace (`forward` false) or Delete (`forward` true) removes when the caret touches a mention: the whole mention
 * rather than one letter. Backspace right after a mention and its separating space removes both. Null when the caret is
 * not at a mention (or text is selected), so the key behaves normally.
 */
export function mentionDeletion(text: string, targets: readonly MentionTarget[], caret: number, forward: boolean): { from: number; to: number } | null {
  for (const { start, end } of mentionSpans(text, targets)) {
    const inside = forward ? caret >= start && caret < end : caret > start && caret <= end;
    if (inside) return { from: start, to: end };
    if (!forward && caret === end + 1 && text[end] === " ") return { from: start, to: caret };
  }
  return null;
}

/**
 * The text a mention takes in the composer: `@Name` for a bot, `/name` for a skill. Plain text keeps the textarea readable
 * and means a hand-typed mention counts the same as one picked from the popover; the daemon never parses it, it receives ids.
 */
export function createMentionFormatter(targets: () => readonly MentionTarget[]): DirectiveFormatter {
  return {
    serialize: (item: TriggerItem) => `${item.type === "skill" ? TRIGGER.skill : TRIGGER.bot}${item.label}`,
    parse(text: string): readonly DirectiveSegment[] {
      const out: DirectiveSegment[] = [];
      let at = 0;
      for (const { start, end, target } of mentionSpans(text, targets())) {
        if (start > at) out.push({ kind: "text", text: text.slice(at, start) });
        out.push({ kind: "mention", type: target.type, label: target.label, id: target.id });
        at = end;
      }
      if (at < text.length) out.push({ kind: "text", text: text.slice(at) });
      return out;
    },
  };
}

/** The bots and skills a message names, deduplicated, in the order they appear. */
export function mentionsOf(text: string, targets: readonly MentionTarget[]): { bots: string[]; skills: string[] } {
  const formatter = createMentionFormatter(() => targets);
  const bots = new Set<string>();
  const skills = new Set<string>();
  for (const segment of formatter.parse(text)) {
    if (segment.kind !== "mention") continue;
    (segment.type === "skill" ? skills : bots).add(segment.id);
  }
  return { bots: [...bots].slice(0, MAX_BOT_MENTIONS), skills: [...skills].slice(0, MAX_SKILL_MENTIONS) };
}

function buildTargets(bots: readonly Bot[], botId: string, skills: readonly string[]): readonly MentionTarget[] {
  return [
    ...bots.filter((b) => b.id !== botId).map((b): MentionTarget => ({ id: b.id, type: "bot", label: b.name, avatar: b.avatar })),
    ...skills.map((name): MentionTarget => ({ id: name, type: "skill", label: name, avatar: "" })),
  ];
}

/** Every name a send can mention, without the registry: other bots and the skills this bot was given (the daemon checks both). */
export function targetsFor(bots: readonly Bot[], botId: string): readonly MentionTarget[] {
  return buildTargets(bots, botId, bots.find((b) => b.id === botId)?.config.skills ?? []);
}

/** Names of registry skills that are switched off; empty outside the desktop or when the registry cannot be read. */
function useDisabledSkills(wanted: boolean): ReadonlySet<string> {
  const [disabled, setDisabled] = useState<ReadonlySet<string>>(new Set());
  useEffect(() => {
    if (!wanted) return;
    let live = true;
    Skills.list().then(
      (list) => { if (live) setDisabled(new Set(list.filter((s) => !s.enabled).map((s) => s.name))); },
      () => {},
    );
    return () => { live = false; };
  }, [wanted]);
  return disabled;
}

/** Mentionable bots (every other bot) and skills (the enabled ones this bot has been given). */
export function useMentionTargets(bot: Pick<Bot, "id" | "config">): readonly MentionTarget[] {
  const bots = useChat((s) => s.bots);
  const disabled = useDisabledSkills(bot.config.skills.length > 0);
  const skills = bot.config.skills;
  return useMemo(() => buildTargets(bots, bot.id, skills.filter((name) => !disabled.has(name))), [bots, bot.id, skills, disabled]);
}
