import { DownloadIcon, EraserIcon, MoreHorizontalIcon, PencilIcon } from "lucide-react";
import { useState } from "react";
import { BotAvatar } from "@/components/shell/BotAvatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Menu, MenuItem, MenuPopup, MenuSeparator, MenuTrigger } from "@/components/ui/menu";
import { ConfirmDialog, toastError } from "@/features/projects/dialogs";
import { Chat, type Bot } from "@/mygo";
import { useChat } from "@/stores/chat";
import { useChatMessages } from "@/stores/chatMessages";
import { toolNoun } from "./parts/format";
import { useBotModel } from "./runtime/useBotModel";

/** The model chip's text: the bot's own model, the default marked as such, or a plain "No model". */
export function ModelChip({ bot }: { bot: Pick<Bot, "provider" | "model"> }) {
  const { label, isDefault } = useBotModel(bot);
  return <Badge variant="outline" size="lg" title="Model" data-slot="model-chip">
    {label ? `${label}${isDefault ? " (default)" : ""}` : "No model chosen"}
  </Badge>;
}

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

export function ConversationHeader({ bot }: { bot: Bot }) {
  const [confirming, setConfirming] = useState(false);
  const tools = bot.config.tools.map((tool) => tool.id);
  return <header data-slot="conversation-header" className="flex min-w-0 shrink-0 items-center gap-3 border-b px-4 py-2">
    <BotAvatar name={bot.name} avatar={bot.avatar} color={bot.color} className="size-8 text-sm" />
    <div className="flex min-w-0 flex-1 flex-col gap-1">
      <h2 className="truncate text-sm font-semibold">{bot.name}</h2>
      <ul aria-label="Model and tools" className="flex min-w-0 flex-wrap items-center gap-1">
        <li><ModelChip bot={bot} /></li>
        {tools.map((id) => <li key={id}><Badge variant="secondary" size="lg">{toolNoun(id)}</Badge></li>)}
      </ul>
    </div>
    <Menu>
      <MenuTrigger render={<Button type="button" variant="ghost" size="icon-sm" aria-label={`More actions for ${bot.name}`} />}><MoreHorizontalIcon aria-hidden /></MenuTrigger>
      <MenuPopup align="end" className="min-w-48">
        <MenuItem onClick={() => useChat.getState().openBuilder(bot.id)}><PencilIcon />Edit bot</MenuItem>
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
  </header>;
}
