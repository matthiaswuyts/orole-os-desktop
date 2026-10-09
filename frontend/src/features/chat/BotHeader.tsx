import { DownloadIcon, EraserIcon, MoreHorizontalIcon, PanelRightIcon, PencilIcon } from "lucide-react";
import { useState } from "react";
import { BotAvatar } from "@/components/shell/BotAvatar";
import { Button } from "@/components/ui/button";
import { Menu, MenuItem, MenuPopup, MenuSeparator, MenuTrigger } from "@/components/ui/menu";
import { ConfirmDialog, toastError } from "@/features/projects/dialogs";
import { Chat, type Bot } from "@/mygo";
import { useChat } from "@/stores/chat";
import { useChatMessages } from "@/stores/chatMessages";

/** Hands the bot's portable settings to the user as a JSON file (never memory, messages or secrets: the daemon decides). */
async function exportBot(bot: Bot) {
  try {
    const exported = await Chat.exportBot(bot.id);
    const url = URL.createObjectURL(new Blob([JSON.stringify(exported, null, 2)], { type: "application/json" }));
    const link = document.createElement("a");
    link.href = url;
    link.download = `${bot.name.replace(/[^\p{L}\p{N}_-]+/gu, "-").replace(/^-+|-+$/g, "") || "bot"}.orole-bot.json`;
    link.click();
    setTimeout(() => URL.revokeObjectURL(url), 0);
  } catch (cause) {
    toastError("Could not export the bot", cause);
  }
}

/** The title bar's content in Chat: the selected bot's avatar and name, nothing else. */
export function BotTitle({ bot }: { bot: Bot }) {
  return <div data-slot="bot-title" className="flex min-w-0 items-center gap-2">
    <BotAvatar name={bot.name} avatar={bot.avatar} className="size-5" />
    <h1 className="truncate font-medium text-sm">{bot.name}</h1>
  </div>;
}

/** The bot's overflow menu: edit, export, clear the conversation. */
export function BotActions({ bot }: { bot: Bot }) {
  const [confirming, setConfirming] = useState(false);
  const open = useChat((s) => s.settingsOpen);
  return <>
    <Button type="button" variant="ghost" size="icon-xs" aria-label={open ? "Hide bot settings" : "Show bot settings"} aria-pressed={open} onClick={() => useChat.getState().setSettingsOpen(!open)}><PanelRightIcon aria-hidden /></Button>
    <Menu>
      <MenuTrigger render={<Button type="button" variant="ghost" size="icon-xs" aria-label={`More actions for ${bot.name}`} />}><MoreHorizontalIcon aria-hidden /></MenuTrigger>
      <MenuPopup align="end" className="min-w-48">
        <MenuItem onClick={() => useChat.getState().setSettingsOpen(true)}><PencilIcon />Bot settings</MenuItem>
        <MenuItem onClick={() => void exportBot(bot)}><DownloadIcon />Export bot</MenuItem>
        <MenuSeparator />
        <MenuItem variant="destructive" onClick={() => setConfirming(true)}><EraserIcon />Clear conversation…</MenuItem>
      </MenuPopup>
    </Menu>
    <ConfirmDialog open={confirming} onOpenChange={setConfirming} title={`Clear conversation with ${bot.name}?`} confirmLabel="Clear conversation"
      onConfirm={async () => {
        if (!(await useChatMessages.getState().clear(bot.id))) throw new Error(useChatMessages.getState().problems[bot.id]?.message ?? "Could not clear the conversation.");
      }}>
      <p>This deletes every message in this conversation. The bot’s memory is kept.</p>
    </ConfirmDialog>
  </>;
}
