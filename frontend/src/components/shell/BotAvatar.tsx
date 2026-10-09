import { avatarUrl } from "@/lib/botAvatar";
import { cn } from "@/lib/utils";

/** A bot's round avatar: a DiceBear "gaze" face generated from the seed saved in `avatar` (the name for older bots). */
export function BotAvatar({ name, avatar, className }: { name: string; avatar: string; className?: string }) {
  return (
    <img src={avatarUrl(avatar.trim() || name.trim() || "bot")} alt="" aria-hidden="true" draggable={false}
      className={cn("inline-block size-5 shrink-0 select-none rounded-full bg-muted object-cover", className)} />
  );
}
