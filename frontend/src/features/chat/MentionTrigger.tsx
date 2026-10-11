import { unstable_useMentionAdapter, type DirectiveFormatter } from "@assistant-ui/react";
import { BotIcon, ScrollTextIcon } from "lucide-react";
import { useMemo } from "react";
import { ComposerTriggerPopover } from "@/components/assistant-ui/elements/composer-trigger-popover";
import { createMentionFormatter, TRIGGER, type MentionTarget } from "./mentions";

const ICONS = { bot: BotIcon, skill: ScrollTextIcon };

/** One picker over one kind of target; selecting writes `@Name` / `/name` and nothing else lists other kinds. */
function KindTrigger({ kind, targets, formatter }: { kind: MentionTarget["type"]; targets: readonly MentionTarget[]; formatter: DirectiveFormatter }) {
  const items = useMemo(() => targets.filter((t) => t.type === kind).map((t) => ({
    id: t.id, type: kind, label: t.label, icon: kind, ...(kind === "bot" ? { metadata: { avatar: t.avatar || t.label } } : {}),
  })), [targets, kind]);
  const mention = unstable_useMentionAdapter({ items, includeModelContextTools: false, formatter, iconMap: ICONS, fallbackIcon: ScrollTextIcon });
  const noun = kind === "bot" ? "bot" : "skill";
  return <ComposerTriggerPopover char={TRIGGER[kind]} {...mention} aria-label={kind === "bot" ? "Mention a bot" : "Use a skill"} className="max-h-56 w-44 overflow-y-auto rounded-md shadow-md"
    emptyCategoriesLabel={`No ${noun}s available`} emptyItemsLabel={`No matching ${noun}`} />;
}

/**
 * `@` in the composer opens a picker of the other bots; `/` opens a picker of this bot's skills. On send the daemon
 * receives ids: each mentioned bot answers in this conversation with it as context, and skills are read before answering.
 */
export function MentionTrigger({ targets }: { targets: readonly MentionTarget[] }) {
  const formatter = useMemo(() => createMentionFormatter(() => targets), [targets]);
  return <>
    <KindTrigger kind="bot" targets={targets} formatter={formatter} />
    <KindTrigger kind="skill" targets={targets} formatter={formatter} />
  </>;
}
