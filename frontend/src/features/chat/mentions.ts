import type { DirectiveFormatter, DirectiveSegment, TriggerItem } from "@assistant-ui/react";
import { useEffect, useMemo } from "react";
import { create } from "zustand";
import { useStoreWithEqualityFn } from "zustand/traditional";
import { MCP, Skills, type Bot, type BotConfig, type ServerState } from "@/mygo";
import { useChat } from "@/stores/chat";

/** What the user can mention in a bot's composer: another bot, one of the bot's integrations, or one of its skills. */
export interface MentionTarget {
  id: string;
  type: "bot" | "skill" | "integration";
  label: string;
  avatar: string;
  /** The one line the picker shows after the name: what the bot or skill is for, or how many tools an integration has. */
  detail: string;
  /** An integration's server, for its logo. */
  server?: ServerState;
}

/** What a thread draws of a bot: its name, avatar, description, skills and integrations. Everything else (preview, run flag, model) is none of its business. */
export type BotInfo = Pick<Bot, "id" | "name" | "avatar" | "description"> & { config: Pick<BotConfig, "skills" | "mcpServers"> };

const sameList = (a: readonly string[], b: readonly string[]) => a.length === b.length && a.every((item, i) => item === b[i]);
const sameInfo = (a: BotInfo, b: BotInfo) =>
  a.id === b.id && a.name === b.name && a.avatar === b.avatar && a.description === b.description && sameList(a.config.skills, b.config.skills) && sameList(a.config.mcpServers, b.config.mcpServers);

/** Every bot, as the same array until one is added, removed or renamed, so a rail preview update or run flag does not wake the thread. */
export function useBotInfos(): readonly BotInfo[] {
  return useStoreWithEqualityFn(useChat, (s) => s.bots, (a, b) => a === b || (a.length === b.length && a.every((bot, i) => sameInfo(bot, b[i]!))));
}

/** Most mentions the daemon accepts per message (internal/chat/mention.go). */
export const MAX_BOT_MENTIONS = 3;
export const MAX_SKILL_MENTIONS = 5;
export const MAX_INTEGRATION_MENTIONS = 5;

const WORD = /[\p{L}\p{N}_-]/u;

/** The character that opens each kind's picker and prefixes it in the text: `@Bot`, `@Integration`, `/skill`. */
export const TRIGGER = { bot: "@", integration: "@", skill: "/" } as const;

/** One mention found in composer text: `text.slice(start, end)` is `@Name` or `/name`. */
export interface MentionSpan { start: number; end: number; target: MentionTarget }

/** Where the known bots, integrations and skills are mentioned in `text`: a trigger at the start or after whitespace, then the exact name. */
export function mentionSpans(text: string, targets: readonly MentionTarget[]): MentionSpan[] {
  // Longest names first so "@Research Bot" is not read as the shorter bot "Research".
  const known = targets.toSorted((a, b) => b.label.length - a.label.length);
  const spans: MentionSpan[] = [];
  for (let i = 0; i < text.length; i++) {
    const trigger = text[i];
    if ((trigger !== "@" && trigger !== "/") || (i > 0 && !/\s/.test(text[i - 1]!))) continue;
    const hit = known.find((t) => {
      const end = i + 1 + t.label.length;
      return TRIGGER[t.type] === trigger && text.startsWith(t.label, i + 1) && (end >= text.length || !WORD.test(text[end]!));
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
 * The text a mention takes in the composer: `@Name` for a bot or an integration, `/name` for a skill. Plain text keeps the
 * textarea readable and means a hand-typed mention counts the same as one picked from the menu; the daemon never parses
 * it, it receives ids.
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

/** The bots, integrations and skills a message names, deduplicated, in the order they appear. */
export function mentionsOf(text: string, targets: readonly MentionTarget[]): { bots: string[]; skills: string[]; integrations: string[] } {
  const found: Record<MentionTarget["type"], Set<string>> = { bot: new Set(), skill: new Set(), integration: new Set() };
  for (const { target } of mentionSpans(text, targets)) found[target.type].add(target.id);
  return {
    bots: [...found.bot].slice(0, MAX_BOT_MENTIONS),
    skills: [...found.skill].slice(0, MAX_SKILL_MENTIONS),
    integrations: [...found.integration].slice(0, MAX_INTEGRATION_MENTIONS),
  };
}

function buildTargets(bots: readonly BotInfo[], botId: string, skills: readonly string[], registry: RegistrySkills, servers: readonly ServerState[]): readonly MentionTarget[] {
  const own = bots.find((b) => b.id === botId)?.config.mcpServers ?? [];
  return [
    ...bots.filter((b) => b.id !== botId).map((b): MentionTarget => ({ id: b.id, type: "bot", label: b.name, avatar: b.avatar, detail: b.description.trim().split("\n", 1)[0] ?? "" })),
    ...servers.filter((s) => s.enabled && own.includes(s.id)).map((s): MentionTarget => ({
      id: s.id, type: "integration", label: s.name, avatar: "", server: s,
      detail: s.status !== "connected" ? "Not connected" : s.toolCount === 1 ? "1 tool" : `${s.toolCount} tools`,
    })),
    ...skills.map((name): MentionTarget => ({ id: name, type: "skill", label: name, avatar: "", detail: (registry.descriptions[name] ?? "").trim().split("\n", 1)[0] ?? "" })),
  ];
}

/** What the registry says about skills: which are switched off, which are built in (every bot has those) and what each is for. */
interface RegistrySkills { disabled: ReadonlySet<string>; builtin: readonly string[]; descriptions: Readonly<Record<string, string>> }
const NO_REGISTRY: RegistrySkills = { disabled: new Set(), builtin: [], descriptions: {} };

/** Shared by the picker, the send path and the message chips so all three agree. Empty outside the desktop or when the registry cannot be read. */
export const useRegistrySkills = create<RegistrySkills>(() => NO_REGISTRY);

function refreshRegistrySkills(): void {
  Skills.list().then(
    (list) => useRegistrySkills.setState({
      disabled: new Set(list.filter((s) => !s.enabled).map((s) => s.name)),
      builtin: list.filter((s) => s.enabled && s.source === "builtin").map((s) => s.name),
      descriptions: Object.fromEntries(list.map((s) => [s.name, s.description])),
    }),
    () => {},
  );
}

/** The registered integrations (MCP servers), shared like the registry skills. Empty outside the desktop or when they cannot be listed. */
export const useIntegrations = create<{ servers: readonly ServerState[] }>(() => ({ servers: [] }));

function refreshIntegrations(): void {
  MCP.list().then((servers) => useIntegrations.setState({ servers: servers ?? [] }), () => {});
}

/** Every name a send can mention, without asking again: other bots, this bot's integrations, the skills it was given and the built-in ones (the daemon checks all). */
export function targetsFor(bots: readonly BotInfo[], botId: string, registry: RegistrySkills = useRegistrySkills.getState(), servers: readonly ServerState[] = useIntegrations.getState().servers): readonly MentionTarget[] {
  const own = bots.find((b) => b.id === botId)?.config.skills ?? [];
  return buildTargets(bots, botId, [...new Set([...own, ...registry.builtin])], registry, servers);
}

/** Mentionable bots (every other bot), integrations (this bot's) and skills (the enabled ones this bot has been given, plus the built-in ones). */
export function useMentionTargets(bot: Pick<Bot, "id" | "config">): readonly MentionTarget[] {
  const bots = useBotInfos();
  const registry = useRegistrySkills();
  const servers = useIntegrations((s) => s.servers);
  useEffect(() => { refreshRegistrySkills(); refreshIntegrations(); }, []);
  const skills = bot.config.skills;
  return useMemo(
    () => buildTargets(bots, bot.id, [...new Set([...skills.filter((name) => !registry.disabled.has(name)), ...registry.builtin])], registry, servers),
    [bots, bot.id, skills, registry, servers],
  );
}
