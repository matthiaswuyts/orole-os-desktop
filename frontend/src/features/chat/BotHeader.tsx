import { PanelRightIcon } from "lucide-react";
import { BotAvatar } from "@/components/shell/BotAvatar";
import { Button } from "@/components/ui/button";
import type { Bot } from "@/mygo";
import { useChat } from "@/stores/chat";

/** The title bar's content in Chat: the selected bot's avatar and name, nothing else. */
export function BotTitle({ bot }: { bot: Bot }) {
  return <div data-slot="bot-title" className="flex min-w-0 items-center gap-2">
    <BotAvatar name={bot.name} avatar={bot.avatar} className="size-5" />
    <h1 className="truncate font-medium text-sm">{bot.name}</h1>
  </div>;
}

/** The title bar's actions in Chat: show or hide the selected bot's settings. */
export function BotActions() {
  const open = useChat((s) => s.settingsOpen);
  return <Button type="button" variant="ghost" size="icon-xs" aria-label={open ? "Hide bot settings" : "Show bot settings"} aria-pressed={open} onClick={() => useChat.getState().setSettingsOpen(!open)}><PanelRightIcon aria-hidden /></Button>;
}
