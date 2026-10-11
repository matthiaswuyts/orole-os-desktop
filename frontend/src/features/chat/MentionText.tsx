import { ScrollTextIcon } from "lucide-react";
import { useMemo } from "react";
import { createDirectiveText } from "@/components/assistant-ui/elements/directive-text";
import { BotAvatar } from "@/components/shell/BotAvatar";
import { createMentionFormatter, type MentionTarget } from "./mentions";

/** A sent message's text with each @bot and /skill drawn as an assistant-ui directive chip: the bot's avatar or a skill icon, then its name. */
export function MentionText({
  text,
  targets,
}: {
  text: string;
  targets: readonly MentionTarget[];
}) {
  const Text = useMemo(
    () =>
      createDirectiveText(
        createMentionFormatter(() => targets),
        {
          // The badge has a fixed height and the directive-text default aligns by baseline; both break a chip that holds an avatar.
          chipClassName:
            "h-auto items-center gap-1.5 rounded-sm bg-background/70 px-3 py-0.5 align-middle text-foreground sm:h-auto",
          renderIcon: (segment) =>
            segment.type === "bot" ? (
              <BotAvatar
                name={segment.label}
                avatar={
                  targets.find((t) => t.type === "bot" && t.id === segment.id)
                    ?.avatar ?? ""
                }
                className="size-4 text-[9px]"
              />
            ) : (
              <ScrollTextIcon aria-hidden className="text-muted-foreground" />
            ),
        },
      ),
    [targets],
  );
  return <Text text={text} />;
}
