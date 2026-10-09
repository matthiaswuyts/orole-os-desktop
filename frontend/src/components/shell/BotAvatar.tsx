import { avatarGlyph, botTone } from "@/lib/botColors";
import { cn } from "@/lib/utils";

/** A bot's round avatar: colour token behind an emoji/initials glyph (never an image: bot data is untrusted). */
export function BotAvatar({ name, avatar, color, className }: { name: string; avatar: string; color: string; className?: string }) {
  return (
    <span aria-hidden="true" className={cn("inline-flex size-5 shrink-0 select-none items-center justify-center rounded-full font-medium text-[10px] leading-none", botTone(color), className)}>
      {avatarGlyph(name, avatar)}
    </span>
  );
}
